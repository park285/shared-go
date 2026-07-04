package dbmigrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWithAdvisoryLockRetriesUntilAcquired(t *testing.T) {
	t.Parallel()

	session := &fakeLockSession{tryResults: []bool{false, false, true}}
	called := false

	err := WithAdvisoryLock(t.Context(), session, LockConfig{
		Key:     42,
		Acquire: 50 * time.Millisecond,
		Poll:    time.Millisecond,
		Release: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		if ctx == nil {
			t.Fatal("fn context is nil")
		}
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAdvisoryLock() error = %v", err)
	}
	if !called {
		t.Fatal("WithAdvisoryLock() did not run fn")
	}
	if session.tryCalls != 3 {
		t.Fatalf("try calls = %d, want 3", session.tryCalls)
	}
	if session.unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", session.unlockCalls)
	}
	if session.keys[0] != 42 || session.keys[len(session.keys)-1] != 42 {
		t.Fatalf("lock keys = %v, want all 42", session.keys)
	}
}

func TestWithAdvisoryLockTimesOut(t *testing.T) {
	t.Parallel()

	session := &fakeLockSession{}
	err := WithAdvisoryLock(t.Context(), session, LockConfig{
		Acquire: 5 * time.Millisecond,
		Poll:    time.Millisecond,
		Release: 5 * time.Millisecond,
	}, func(context.Context) error {
		t.Fatal("fn must not run without lock")
		return nil
	})
	if err == nil {
		t.Fatal("WithAdvisoryLock() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("WithAdvisoryLock() error = %v, want timeout", err)
	}
	if session.unlockCalls != 0 {
		t.Fatalf("unlock calls = %d, want 0", session.unlockCalls)
	}
}

func TestWithAdvisoryLockUnlocksWhenFunctionErrors(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")
	session := &fakeLockSession{tryResults: []bool{true}}
	err := WithAdvisoryLock(t.Context(), session, LockConfig{
		Acquire: 20 * time.Millisecond,
		Poll:    time.Millisecond,
		Release: 20 * time.Millisecond,
	}, func(context.Context) error {
		return runErr
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("WithAdvisoryLock() error = %v, want run error", err)
	}
	if session.unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", session.unlockCalls)
	}
}

func TestWithAdvisoryLockUnlockErrorHandling(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")
	unlockErr := errors.New("unlock failed")
	tests := []struct {
		name                string
		runErr              error
		unlockErr           error
		unlockReleased      bool
		unlockReleasedSet   bool
		onUnlockError       bool
		wantReturnErr       error
		wantCallbackErr     bool
		wantNotHeld         bool
		wantCallbackNotHeld bool
	}{
		{
			name:          "unlock error",
			unlockErr:     unlockErr,
			wantReturnErr: unlockErr,
		},
		{
			name:              "unlock released false",
			unlockReleased:    false,
			unlockReleasedSet: true,
			wantNotHeld:       true,
		},
		{
			name:          "run and unlock error",
			runErr:        runErr,
			unlockErr:     unlockErr,
			wantReturnErr: runErr,
		},
		{
			name:            "callback keeps nil return",
			unlockErr:       unlockErr,
			onUnlockError:   true,
			wantCallbackErr: true,
		},
		{
			name:                "callback receives released false",
			unlockReleased:      false,
			unlockReleasedSet:   true,
			onUnlockError:       true,
			wantCallbackErr:     true,
			wantCallbackNotHeld: true,
		},
		{
			name:            "callback keeps run error",
			runErr:          runErr,
			unlockErr:       unlockErr,
			onUnlockError:   true,
			wantReturnErr:   runErr,
			wantCallbackErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := &fakeLockSession{
				tryResults:        []bool{true},
				unlockErr:         tt.unlockErr,
				unlockReleased:    tt.unlockReleased,
				unlockReleasedSet: tt.unlockReleasedSet,
			}
			var callbackErr error
			cfg := LockConfig{
				Acquire: 20 * time.Millisecond,
				Poll:    time.Millisecond,
				Release: 20 * time.Millisecond,
			}
			if tt.onUnlockError {
				cfg.OnUnlockError = func(err error) {
					callbackErr = err
				}
			}

			err := WithAdvisoryLock(t.Context(), session, cfg, func(context.Context) error {
				return tt.runErr
			})
			if tt.wantReturnErr == nil && !tt.wantNotHeld {
				if err != nil {
					t.Fatalf("WithAdvisoryLock() error = %v, want nil", err)
				}
			} else if tt.wantReturnErr != nil && !errors.Is(err, tt.wantReturnErr) {
				t.Fatalf("WithAdvisoryLock() error = %v, want %v", err, tt.wantReturnErr)
			}
			if tt.runErr != nil && tt.unlockErr != nil && !tt.onUnlockError && !errors.Is(err, tt.unlockErr) {
				t.Fatalf("WithAdvisoryLock() error = %v, want joined unlock error", err)
			}
			if tt.wantNotHeld && (err == nil || !strings.Contains(err.Error(), "lock was not held")) {
				t.Fatalf("WithAdvisoryLock() error = %v, want lock not held", err)
			}
			if tt.wantCallbackErr {
				if callbackErr == nil {
					t.Fatal("OnUnlockError() error = nil, want unlock error")
				}
				if tt.unlockErr != nil && !errors.Is(callbackErr, tt.unlockErr) {
					t.Fatalf("OnUnlockError() error = %v, want %v", callbackErr, tt.unlockErr)
				}
				if tt.wantCallbackNotHeld && !strings.Contains(callbackErr.Error(), "lock was not held") {
					t.Fatalf("OnUnlockError() error = %v, want lock not held", callbackErr)
				}
			} else if callbackErr != nil {
				t.Fatalf("OnUnlockError() error = %v, want nil", callbackErr)
			}
			if session.unlockCalls != 1 {
				t.Fatalf("unlock calls = %d, want 1", session.unlockCalls)
			}
		})
	}
}

func TestWithAdvisoryLockExternalContextCancellationDuringPoll(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	session := &fakeLockSession{
		tryResults: []bool{false},
		afterTry:   cancel,
	}
	err := WithAdvisoryLock(ctx, session, LockConfig{
		Acquire: time.Second,
		Poll:    time.Hour,
		Release: 5 * time.Millisecond,
	}, func(context.Context) error {
		t.Fatal("fn must not run without lock")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithAdvisoryLock() error = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("WithAdvisoryLock() error = %v, want cancellation message", err)
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("WithAdvisoryLock() error = %v, want cancellation message", err)
	}
	if session.unlockCalls != 0 {
		t.Fatalf("unlock calls = %d, want 0", session.unlockCalls)
	}
}

func TestWithAdvisoryLockPanicStillUnlocks(t *testing.T) {
	t.Parallel()

	session := &fakeLockSession{tryResults: []bool{true}}
	didPanic := false
	func() {
		defer func() {
			didPanic = recover() != nil
		}()
		_ = WithAdvisoryLock(t.Context(), session, LockConfig{
			Acquire: 20 * time.Millisecond,
			Poll:    time.Millisecond,
			Release: 20 * time.Millisecond,
		}, func(context.Context) error {
			panic("boom")
		})
	}()
	if !didPanic {
		t.Fatal("WithAdvisoryLock() did not panic")
	}
	if session.unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", session.unlockCalls)
	}
}

type fakeLockSession struct {
	tryResults        []bool
	afterTry          func()
	tryCalls          int
	unlockCalls       int
	keys              []int64
	unlockErr         error
	unlockReleased    bool
	unlockReleasedSet bool
}

func (s *fakeLockSession) TryAdvisoryLock(_ context.Context, key int64) (bool, error) {
	s.keys = append(s.keys, key)
	s.tryCalls++
	defer func() {
		if s.afterTry != nil {
			s.afterTry()
		}
	}()
	if s.tryCalls <= len(s.tryResults) {
		return s.tryResults[s.tryCalls-1], nil
	}
	return false, nil
}

func (s *fakeLockSession) AdvisoryUnlock(_ context.Context, key int64) (bool, error) {
	s.keys = append(s.keys, key)
	s.unlockCalls++
	released := true
	if s.unlockReleasedSet {
		released = s.unlockReleased
	}
	return released, s.unlockErr
}
