package lifecycle

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestRun_StopsOnSignalAndRunsShutdownWithTimeout(t *testing.T) {
	t.Parallel()

	const shutdownBudget = 50 * time.Millisecond

	signalCh := make(chan os.Signal, 1)
	cancelObserved := make(chan struct{})

	var (
		stopNotifyCalled atomic.Bool
		gotSignal        os.Signal
		beforeShutdown   atomic.Bool
		gotSignals       []os.Signal
	)

	err := Run(t.Context(), Options{
		ShutdownTimeout: shutdownBudget,
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
			assertShutdownDeadlineWithin(t, deadline, ok, shutdownBudget)

			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	assertRunContextCanceled(t, cancelObserved)

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

func assertShutdownDeadlineWithin(t *testing.T, deadline time.Time, ok bool, budget time.Duration) {
	t.Helper()

	if !ok {
		t.Fatal("shutdown context missing deadline")
	}

	until := time.Until(deadline)
	if until <= 0 || until > budget {
		t.Fatalf("shutdown deadline remaining = %v, want within (0, %v]", until, budget)
	}
}

func assertRunContextCanceled(t *testing.T, canceled <-chan struct{}) {
	t.Helper()

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("run context was not canceled")
	}
}

func TestRun_StopsOnRuntimeErrorAndReturnsRuntimeAndShutdownErrors(t *testing.T) {
	t.Parallel()

	runtimeErr := errors.New("runtime boom")
	shutdownErr := errors.New("shutdown boom")

	var gotErr error

	err := Run(t.Context(), Options{
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
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Run() error = %v, want runtime error %v", err, runtimeErr)
	}

	if !errors.Is(err, shutdownErr) {
		t.Fatalf("Run() error = %v, want shutdown error %v", err, shutdownErr)
	}

	if !errors.Is(gotErr, runtimeErr) {
		t.Fatalf("OnError() error = %v, want %v", gotErr, runtimeErr)
	}
}

func TestRun_NilContext(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // nil ctx가 Background로 대체되는 방어 경로를 검증한다.
	err := Run(nil, Options{
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

	err := Run(t.Context(), Options{
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

	startedAt := time.Now()

	err := Run(t.Context(), Options{
		ShutdownTimeout: 0,
		NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
			return make(chan os.Signal), func() {}
		},
		Start: func(_ context.Context, errCh chan<- error) {
			errCh <- nil
		},
		Shutdown: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("shutdown context must carry the default deadline when timeout is zero")

				return nil
			}

			if budget := deadline.Sub(startedAt); budget <= 0 || budget > DefaultShutdownTimeout+time.Second {
				t.Fatalf("shutdown budget = %v, want bounded by DefaultShutdownTimeout", budget)
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

	err := Run(t.Context(), Options{
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

func TestRun_ParentContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	err := Run(ctx, Options{
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

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Run(ctx, Options{
		Start: nil,
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

	err := Run(t.Context(), Options{
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

func TestDrainRuntimeError_FiresOnErrorWhenErrorPending(t *testing.T) {
	t.Parallel()

	pendingErr := errors.New("pending runtime error")
	errCh := make(chan error, 1)

	errCh <- pendingErr

	var gotErr error

	drainedErr := drainRuntimeError(errCh, func(err error) { gotErr = err })

	if !errors.Is(gotErr, pendingErr) {
		t.Fatalf("OnError() error = %v, want %v", gotErr, pendingErr)
	}

	if !errors.Is(drainedErr, pendingErr) {
		t.Fatalf("drainRuntimeError() error = %v, want %v", drainedErr, pendingErr)
	}
}

func TestDrainRuntimeError_NoOpWhenEmpty(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	called := false
	drainedErr := drainRuntimeError(errCh, func(error) { called = true })

	if called {
		t.Fatal("OnError() should not fire when errCh is empty")
	}

	if drainedErr != nil {
		t.Fatalf("drainRuntimeError() error = %v, want nil", drainedErr)
	}
}

func TestDrainRuntimeError_SkipsNilError(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	errCh <- nil

	called := false
	drainedErr := drainRuntimeError(errCh, func(error) { called = true })

	if called {
		t.Fatal("OnError() should not fire for a nil error")
	}

	if drainedErr != nil {
		t.Fatalf("drainRuntimeError() error = %v, want nil", drainedErr)
	}
}

func TestRun_SignalWithPendingErrorStillFiresOnError(t *testing.T) {
	t.Parallel()

	const iterations = 30

	runtimeErr := errors.New("runtime boom on signal race")

	for i := range iterations {
		var (
			onErrorCount atomic.Int32
			gotErr       atomic.Value
		)

		signalCh := make(chan os.Signal, 1)

		err := Run(t.Context(), Options{
			NotifySignals: func(...os.Signal) (<-chan os.Signal, func()) {
				return signalCh, func() {}
			},
			Start: func(_ context.Context, errCh chan<- error) {
				errCh <- runtimeErr

				signalCh <- syscall.SIGTERM
			},
			OnError: func(err error) {
				onErrorCount.Add(1)
				gotErr.Store(err)
			},
			Shutdown: func(context.Context) error { return nil },
		})
		if !errors.Is(err, runtimeErr) {
			t.Fatalf("iteration %d: Run() error = %v, want %v", i, err, runtimeErr)
		}

		if onErrorCount.Load() != 1 {
			t.Fatalf("iteration %d: OnError() called %d times, want exactly 1", i, onErrorCount.Load())
		}

		stored := testsupport.AssertType[error](t, "gotErr.Load()", gotErr.Load())
		if !errors.Is(stored, runtimeErr) {
			t.Fatalf("iteration %d: OnError() error = %v, want %v", i, stored, runtimeErr)
		}
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
