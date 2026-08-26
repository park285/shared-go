package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// DefaultShutdownTimeout은 Options.ShutdownTimeout이 0 이하일 때 적용하는 보수적 기본 종료 예산이다.
const DefaultShutdownTimeout = 30 * time.Second

type SignalNotifier func(signals ...os.Signal) (<-chan os.Signal, func())

type Options struct {
	ShutdownTimeout time.Duration
	Signals         []os.Signal
	NotifySignals   SignalNotifier
	Start           func(ctx context.Context, errCh chan<- error)
	OnSignal        func(os.Signal)
	OnError         func(error)
	BeforeShutdown  func()
	Shutdown        func(ctx context.Context) error
}

func Run(ctx context.Context, opts Options) error {
	baseCtx := baseContext(ctx)
	sigCh, stopSignals := signalSubscription(opts.NotifySignals, opts.Signals)
	stopSignals = sync.OnceFunc(stopSignals)
	defer stopSignals()

	runCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	errCh := make(chan error, 1)
	startRuntime(runCtx, opts.Start, errCh)

	runtimeErr := waitForStop(baseCtx, sigCh, errCh, opts.OnSignal, opts.OnError)
	stopSignals()
	beforeShutdown(opts.BeforeShutdown)

	cancel()

	shutdownCtx, shutdownCancel := shutdownContext(baseCtx, opts.ShutdownTimeout)
	defer shutdownCancel()

	shutdownErr := shutdown(shutdownCtx, opts.Shutdown)

	switch {
	case runtimeErr == nil && shutdownErr == nil:
		return nil
	case runtimeErr == nil:
		return fmt.Errorf("shutdown: %w", shutdownErr)
	case shutdownErr == nil:
		return fmt.Errorf("run: %w", runtimeErr)
	}

	return fmt.Errorf("run: %w", errors.Join(runtimeErr, shutdownErr))
}

func baseContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}

	return context.Background()
}

func signalSubscription(notifySignals SignalNotifier, signals []os.Signal) (<-chan os.Signal, func()) {
	if notifySignals == nil {
		notifySignals = defaultSignalNotifier
	}

	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}

	sigCh, stopSignals := notifySignals(signals...)
	if stopSignals != nil {
		return sigCh, stopSignals
	}

	return sigCh, func() {}
}

func startRuntime(ctx context.Context, start func(ctx context.Context, errCh chan<- error), errCh chan<- error) {
	if start != nil {
		start(ctx, errCh)
	}
}

func waitForStop(
	baseCtx context.Context,
	sigCh <-chan os.Signal,
	errCh <-chan error,
	onSignal func(os.Signal),
	onError func(error),
) error {
	select {
	case sig := <-sigCh:
		handleSignal(onSignal, sig)

		if err := drainRuntimeError(errCh, onError); err != nil {
			return fmt.Errorf("drain runtime error: %w", err)
		}

		return nil
	case err := <-errCh:
		if err != nil {
			handleRuntimeError(onError, err)
		}

		return err
	case <-baseCtx.Done():
		if err := drainRuntimeError(errCh, onError); err != nil {
			return fmt.Errorf("drain runtime error: %w", err)
		}

		return nil
	}
}

func drainRuntimeError(errCh <-chan error, onError func(error)) error {
	select {
	case err := <-errCh:
		if err != nil {
			handleRuntimeError(onError, err)
		}

		return err
	default:
		return nil
	}
}

func handleSignal(fn func(os.Signal), sig os.Signal) {
	if fn != nil {
		fn(sig)
	}
}

func handleRuntimeError(fn func(error), err error) {
	if fn != nil {
		fn(err)
	}
}

func beforeShutdown(fn func()) {
	if fn != nil {
		fn()
	}
}

// 종료 절차는 baseCtx가 이미 취소된 뒤에 실행되므로 취소만 끊고 값은 그대로 물려받는다.
func shutdownContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}

	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func shutdown(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return nil
	}

	if err := fn(ctx); err != nil {
		return fmt.Errorf("shutdown callback: %w", err)
	}

	return nil
}

func defaultSignalNotifier(signals ...os.Signal) (<-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)

	return sigCh, func() {
		signal.Stop(sigCh)
	}
}
