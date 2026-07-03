package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestComputeBackoffDelay_ExponentialGrowth(t *testing.T) {
	base := 100 * time.Millisecond

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
	}

	for _, tt := range tests {
		got := ComputeBackoffDelay(tt.attempt, base, 0)
		if got != tt.expected {
			t.Errorf("ComputeBackoffDelay(%d, %v, 0) = %v, want %v", tt.attempt, base, got, tt.expected)
		}
	}
}

func TestComputeBackoffDelay_WithJitter(t *testing.T) {
	base := 100 * time.Millisecond
	jitter := 50 * time.Millisecond

	for range 100 {
		delay := ComputeBackoffDelay(0, base, jitter)
		if delay < base || delay >= base+jitter {
			t.Errorf("delay %v outside expected range [%v, %v)", delay, base, base+jitter)
		}
	}
}

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	callCount := 0
	fakeSleep := func(_ context.Context, _ time.Duration) bool {
		t.Error("sleep should not be called on first success")
		return true
	}

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		Sleep:       fakeSleep,
	}, func(_ context.Context) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	sleepCount := 0
	targetErr := errors.New("transient error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			sleepCount++
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		if callCount < 3 {
			return targetErr
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
	if sleepCount != 2 {
		t.Errorf("expected 2 sleeps, got %d", sleepCount)
	}
}

func TestWithRetry_AllAttemptsFail(t *testing.T) {
	callCount := 0
	targetErr := errors.New("persistent error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Errorf("expected targetErr, got %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestWithRetry_ShouldRetryFalse(t *testing.T) {
	callCount := 0
	permanentErr := errors.New("permanent error")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		ShouldRetry: func(_ error) bool {
			return false
		},
		Sleep: func(_ context.Context, _ time.Duration) bool {
			t.Error("sleep should not be called when ShouldRetry returns false")
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return permanentErr
	})

	if !errors.Is(err, permanentErr) {
		t.Errorf("expected permanentErr, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	err := WithRetry(ctx, RetryOptions{
		MaxAttempts: 5,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			if callCount >= 2 {
				cancel()
				return false
			}
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return errors.New("error")
	})

	if err == nil {
		t.Error("expected error after context cancellation")
	}
	if callCount > 3 {
		t.Errorf("too many calls after cancellation: %d", callCount)
	}
}

func TestWithRetry_OnRetryCallback(t *testing.T) {
	retryAttempts := []int{}

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		OnRetry: func(attempt int, _ error, _ time.Duration) {
			retryAttempts = append(retryAttempts, attempt)
		},
		Sleep: func(_ context.Context, _ time.Duration) bool {
			return true
		},
	}, func(_ context.Context) error {
		return errors.New("error")
	})
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}

	if len(retryAttempts) != 2 {
		t.Errorf("expected 2 OnRetry calls, got %d", len(retryAttempts))
	}
	if retryAttempts[0] != 1 || retryAttempts[1] != 2 {
		t.Errorf("unexpected retry attempts: %v", retryAttempts)
	}
}

func TestWithRetry_Jitter0IsDeterministic(t *testing.T) {
	targetErr := errors.New("transient")
	var slept []time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 4,
		BaseDelay:   100 * time.Millisecond,
		Jitter:      0,
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = append(slept, d)
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if len(slept) != len(want) {
		t.Fatalf("expected %d sleeps, got %d (%v)", len(want), len(slept), slept)
	}
	for i, w := range want {
		if slept[i] != w {
			t.Errorf("sleep[%d] = %v, want %v", i, slept[i], w)
		}
	}
}

func TestWithRetry_JitterWidensDelayWithinRange(t *testing.T) {
	targetErr := errors.New("transient")
	base := 100 * time.Millisecond
	jitter := 50 * time.Millisecond
	var slept []time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 4,
		BaseDelay:   base,
		Jitter:      jitter,
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = append(slept, d)
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if len(slept) != 3 {
		t.Fatalf("expected 3 sleeps, got %d", len(slept))
	}
	for attempt, d := range slept {
		lo := base << attempt
		if d < lo || d >= lo+jitter {
			t.Errorf("sleep[%d] = %v outside [%v, %v)", attempt, d, lo, lo+jitter)
		}
	}
}

func TestWithRetry_MaxDelayCapsComputedDelay(t *testing.T) {
	targetErr := errors.New("transient")
	var slept []time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    1500 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = append(slept, d)
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if len(slept) != 2 {
		t.Fatalf("expected 2 sleeps, got %d", len(slept))
	}
	if slept[0] != time.Second {
		t.Fatalf("first delay = %v, want 1s", slept[0])
	}
	if slept[1] != 1500*time.Millisecond {
		t.Fatalf("second delay = %v, want capped 1.5s", slept[1])
	}
}

func TestWithRetry_DelayOverrideWinsBeforeMaxDelay(t *testing.T) {
	targetErr := errors.New("retry-after")
	var slept time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 2,
		BaseDelay:   time.Second,
		MaxDelay:    3 * time.Second,
		DelayOverride: func(err error, _ time.Duration) (time.Duration, bool) {
			if errors.Is(err, targetErr) {
				return 10 * time.Second, true
			}
			return 0, false
		},
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if slept != 3*time.Second {
		t.Fatalf("delay = %v, want max-delay capped 3s", slept)
	}
}

func TestWithRetry_DelayOverrideUsedWhenBelowMaxDelay(t *testing.T) {
	targetErr := errors.New("retry-after")
	var slept time.Duration

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 2,
		BaseDelay:   5 * time.Second,
		MaxDelay:    10 * time.Second,
		DelayOverride: func(_ error, _ time.Duration) (time.Duration, bool) {
			return 250 * time.Millisecond, true
		},
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept = d
			return true
		},
	}, func(_ context.Context) error {
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if slept != 250*time.Millisecond {
		t.Fatalf("delay = %v, want override 250ms", slept)
	}
}

func TestWithRetry_ContextCancelPropagatesViaSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	targetErr := errors.New("transient")

	err := WithRetry(ctx, RetryOptions{
		MaxAttempts: 5,
		BaseDelay:   time.Second,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			cancel()
			return false
		},
	}, func(_ context.Context) error {
		callCount++
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected last error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call before cancel-aborted sleep, got %d", callCount)
	}
}

func TestWithRetry_ContextErrorReturnedWhenNoPriorError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	callCount := 0

	err := WithRetry(ctx, RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if callCount != 0 {
		t.Errorf("fn should not run under a cancelled context, got %d calls", callCount)
	}
}

func TestWithRetry_MaxAttemptsNormalizedToOne(t *testing.T) {
	callCount := 0
	targetErr := errors.New("boom")

	err := WithRetry(context.Background(), RetryOptions{
		MaxAttempts: 0,
		BaseDelay:   time.Millisecond,
		Sleep: func(_ context.Context, _ time.Duration) bool {
			t.Error("sleep should not run with a single normalized attempt")
			return true
		},
	}, func(_ context.Context) error {
		callCount++
		return targetErr
	})

	if !errors.Is(err, targetErr) {
		t.Fatalf("expected target error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}
