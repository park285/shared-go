package httputil

import (
	"testing"
	"time"
)

func TestLoginFailureRateLimiter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxAttempts: 3,
		Window:      time.Minute,
		Lockout:     5 * time.Minute,
		Now:         func() time.Time { return now },
	})

	tests := []struct {
		name       string
		action     func() (bool, time.Duration, int)
		wantAllow  bool
		wantRetry  bool
		wantCount  int
		checkCount bool
	}{
		{
			name: "initial allowed",
			action: func() (bool, time.Duration, int) {
				allowed, retry := limiter.IsAllowed("203.0.113.1")
				return allowed, retry, 0
			},
			wantAllow: true,
		},
		{
			name: "failure one",
			action: func() (bool, time.Duration, int) {
				return false, 0, limiter.RecordFailure("203.0.113.1")
			},
			wantCount:  1,
			checkCount: true,
		},
		{
			name: "failure two",
			action: func() (bool, time.Duration, int) {
				return false, 0, limiter.RecordFailure("203.0.113.1")
			},
			wantCount:  2,
			checkCount: true,
		},
		{
			name: "failure three locks",
			action: func() (bool, time.Duration, int) {
				return false, 0, limiter.RecordFailure("203.0.113.1")
			},
			wantCount:  3,
			checkCount: true,
		},
		{
			name: "locked",
			action: func() (bool, time.Duration, int) {
				allowed, retry := limiter.IsAllowed("203.0.113.1")
				return allowed, retry, 0
			},
			wantAllow: false,
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, retry, count := tt.action()
			if tt.checkCount {
				if count != tt.wantCount {
					t.Fatalf("count = %d, want %d", count, tt.wantCount)
				}
				return
			}
			if allowed != tt.wantAllow {
				t.Fatalf("allowed = %t, want %t", allowed, tt.wantAllow)
			}
			if (retry > 0) != tt.wantRetry {
				t.Fatalf("retry = %s, want positive=%t", retry, tt.wantRetry)
			}
		})
	}

	now = now.Add(6 * time.Minute)
	allowed, retry := limiter.IsAllowed("203.0.113.1")
	if !allowed || retry != 0 {
		t.Fatalf("after lockout allowed=%t retry=%s, want true/0", allowed, retry)
	}
}

func TestLoginFailureRateLimiterSuccessAndCleanup(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxAttempts: 2,
		Window:      time.Minute,
		Lockout:     5 * time.Minute,
		Now:         func() time.Time { return now },
	})

	if got := limiter.RecordFailure("203.0.113.2"); got != 1 {
		t.Fatalf("RecordFailure() = %d, want 1", got)
	}
	limiter.RecordSuccess("203.0.113.2")
	if got := limiter.RecordFailure("203.0.113.2"); got != 1 {
		t.Fatalf("RecordFailure() after success = %d, want 1", got)
	}

	limiter.RecordFailure("203.0.113.3")
	now = now.Add(2 * time.Minute)
	limiter.cleanupStale(now)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if _, ok := limiter.attempts["203.0.113.3"]; ok {
		t.Fatal("cleanupStale() kept expired non-locked entry")
	}
}

func TestLoginFailureRateLimiterStartStop(t *testing.T) {
	t.Parallel()

	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{})
	limiter.Start()
	requireStopReturns(t, limiter)
	requireStopReturns(t, limiter)
}

func TestLoginFailureRateLimiterStopWithoutStart(t *testing.T) {
	t.Parallel()

	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{})
	requireStopReturns(t, limiter)
	requireStopReturns(t, limiter)
}

func requireStopReturns(t *testing.T, limiter *LoginFailureRateLimiter) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		limiter.Stop()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return")
	}
}
