package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultShutdownTimeout은 Options.ShutdownTimeout이 0 이하일 때 적용하는 보수적 기본 종료 예산이다.
const DefaultShutdownTimeout = 30 * time.Second

type SignalNotifier func(signals ...os.Signal) (<-chan os.Signal, func())

type Options struct {
	BaseContext     context.Context
	ShutdownTimeout time.Duration
	Signals         []os.Signal
	NotifySignals   SignalNotifier
	Start           func(ctx context.Context, errCh chan<- error)
	OnSignal        func(os.Signal)
	OnError         func(error)
	BeforeShutdown  func()
	Shutdown        func(ctx context.Context) error
}

func Run(opts Options) error {
	baseCtx := baseContext(opts.BaseContext)
	sigCh, stopSignals := signalSubscription(opts.NotifySignals, opts.Signals)
	defer stopSignals()

	runCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	errCh := make(chan error, 1)
	startRuntime(opts.Start, runCtx, errCh)
	runtimeErr := waitForStop(baseCtx, sigCh, errCh, opts.OnSignal, opts.OnError)
	beforeShutdown(opts.BeforeShutdown)

	cancel()

	shutdownCtx, shutdownCancel := shutdownContext(opts.ShutdownTimeout)
	defer shutdownCancel()

	shutdownErr := shutdown(opts.Shutdown, shutdownCtx)
	if runtimeErr == nil {
		return shutdownErr
	}
	if shutdownErr == nil {
		return runtimeErr
	}
	return errors.Join(runtimeErr, shutdownErr)
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

func startRuntime(start func(ctx context.Context, errCh chan<- error), ctx context.Context, errCh chan<- error) {
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
		return drainRuntimeError(errCh, onError)
	case err := <-errCh:
		handleRuntimeError(onError, err)
		return err
	case <-baseCtx.Done():
		return drainRuntimeError(errCh, onError)
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

func shutdownContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func shutdown(fn func(ctx context.Context) error, ctx context.Context) error {
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func defaultSignalNotifier(signals ...os.Signal) (<-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)
	return sigCh, func() {
		signal.Stop(sigCh)
	}
}
