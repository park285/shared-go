package loop

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunTickerLoop_ContextCancelReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RunTickerLoop(ctx, time.Millisecond, func(context.Context) error {
		t.Fatal("onTick should not run after context cancellation")
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunTickerLoop() error = %v, want context.Canceled", err)
	}
}

func TestRunTickerLoop_OnTickErrorTerminatesLoop(t *testing.T) {
	wantErr := errors.New("tick failed")
	var calls int32

	err := RunTickerLoop(context.Background(), time.Millisecond, func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("RunTickerLoop() error = %v, want %v", err, wantErr)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("onTick calls = %d, want 1", got)
	}
}

func TestRunTickerLoop_InvalidIntervalReturnsError(t *testing.T) {
	err := RunTickerLoop(context.Background(), 0, func(context.Context) error {
		t.Fatal("onTick should not run with invalid interval")
		return nil
	})

	if err == nil {
		t.Fatal("RunTickerLoop() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "interval") {
		t.Fatalf("RunTickerLoop() error = %q, want interval context", err)
	}
}

func TestRunTickerLoop_NilOnTickReturnsError(t *testing.T) {
	err := RunTickerLoop(context.Background(), time.Millisecond, nil)

	if err == nil {
		t.Fatal("RunTickerLoop() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "onTick") {
		t.Fatalf("RunTickerLoop() error = %q, want onTick context", err)
	}
}

func TestRunTickerLoop_TicksAtInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	var calls int32

	go func() {
		done <- RunTickerLoop(ctx, 5*time.Millisecond, func(context.Context) error {
			if atomic.AddInt32(&calls, 1) == 3 {
				cancel()
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunTickerLoop() error = %v, want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("RunTickerLoop() did not stop after context cancellation")
	}

	if got := atomic.LoadInt32(&calls); got < 3 {
		t.Fatalf("onTick calls = %d, want at least 3", got)
	}
}
