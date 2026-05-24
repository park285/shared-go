package lifecycle

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestRun_StopsOnSignalAndRunsShutdownWithTimeout(t *testing.T) {
	t.Parallel()

	signalCh := make(chan os.Signal, 1)
	cancelObserved := make(chan struct{})
	var stopNotifyCalled atomic.Bool
	var gotSignal os.Signal
	var beforeShutdown atomic.Bool
	var gotSignals []os.Signal

	err := Run(Options{
		ShutdownTimeout: 50 * time.Millisecond,
		NotifySignals: func(signals ...os.Signal) (<-chan os.Signal, func()) {
			gotSignals = append([]os.Signal(nil), signals...)
			return signalCh, func() {
				stopNotifyCalled.Store(true)
			}
		},
		Start: func(ctx context.Context, _ chan<- error) {
			go func() {
				<-ctx.Done()
				close(cancelObserved)
			}()
			signalCh <- syscall.SIGTERM
		},
		OnSignal: func(sig os.Signal) {
			gotSignal = sig
		},
		BeforeShutdown: func() {
			beforeShutdown.Store(true)
		},
		Shutdown: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("shutdown context missing deadline")
			}
			until := time.Until(deadline)
			if until <= 0 || until > 50*time.Millisecond {
				t.Fatalf("shutdown deadline remaining = %v, want within (0, 50ms]", until)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("run context was not canceled")
	}

	if len(gotSignals) != 2 || gotSignals[0] != syscall.SIGINT || gotSignals[1] != syscall.SIGTERM {
		t.Fatalf("NotifySignals() signals = %v, want [SIGINT SIGTERM]", gotSignals)
	}
	if gotSignal != syscall.SIGTERM {
		t.Fatalf("OnSignal() signal = %v, want SIGTERM", gotSignal)
	}
	if !beforeShutdown.Load() {
		t.Fatal("BeforeShutdown() was not called")
	}
	if !stopNotifyCalled.Load() {
		t.Fatal("signal stop function was not called")
	}
}

func TestRun_StopsOnRuntimeErrorAndReturnsShutdownError(t *testing.T) {
	t.Parallel()

	runtimeErr := errors.New("runtime boom")
	shutdownErr := errors.New("shutdown boom")
	var gotErr error

	err := Run(Options{
		ShutdownTimeout: 50 * time.Millisecond,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- runtimeErr
		},
		OnError: func(err error) {
			gotErr = err
		},
		Shutdown: func(context.Context) error {
			return shutdownErr
		},
	})
	if !errors.Is(err, shutdownErr) {
		t.Fatalf("Run() error = %v, want %v", err, shutdownErr)
	}
	if !errors.Is(gotErr, runtimeErr) {
		t.Fatalf("OnError() error = %v, want %v", gotErr, runtimeErr)
	}
}

func TestRun_NilBaseContext(t *testing.T) {
	t.Parallel()

	err := Run(Options{
		BaseContext: nil,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_NilShutdown(t *testing.T) {
	t.Parallel()

	err := Run(Options{
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- nil
		},
		Shutdown: nil,
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_ZeroShutdownTimeout(t *testing.T) {
	t.Parallel()

	err := Run(Options{
		ShutdownTimeout: 0,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- nil
		},
		Shutdown: func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); ok {
				t.Fatal("shutdown context should not have a deadline when timeout is zero")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_NilStopSignalsReturn(t *testing.T) {
	t.Parallel()

	err := Run(Options{
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), nil
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_BaseContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	err := Run(Options{
		BaseContext: ctx,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, _ chan<- error) {
			cancel()
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_NilStartCallback(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(Options{
		BaseContext: ctx,
		Start:       nil,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func TestRun_CustomSignals(t *testing.T) {
	t.Parallel()

	var gotSignals []os.Signal

	err := Run(Options{
		Signals: []os.Signal{syscall.SIGUSR1},
		NotifySignals: func(signals ...os.Signal) (<-chan os.Signal, func()) {
			gotSignals = append([]os.Signal(nil), signals...)
			ch := make(chan os.Signal, 1)
			ch <- syscall.SIGUSR1
			return ch, func() {}
		},
		Start: func(_ context.Context, _ chan<- error) {},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(gotSignals) != 1 || gotSignals[0] != syscall.SIGUSR1 {
		t.Fatalf("NotifySignals() signals = %v, want [SIGUSR1]", gotSignals)
	}
}

func TestDefaultSignalNotifier(t *testing.T) {
	t.Parallel()

	sigCh, stop := defaultSignalNotifier(syscall.SIGUSR1)
	defer stop()

	if sigCh == nil {
		t.Fatal("signal channel is nil")
	}
}
