package httputil

import (
	"fmt"
	"sync"
	"sync/atomic"
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

func TestLoginFailureRateLimiterIsAllowedReservesCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxIdentities: 1,
		Window:        time.Minute,
		Now:           func() time.Time { return now },
	})

	if allowed, _ := limiter.IsAllowed("first"); !allowed {
		t.Fatal("IsAllowed(first) = false")
	}
	if allowed, retry := limiter.IsAllowed("second"); allowed || retry <= 0 {
		t.Fatalf("IsAllowed(second) = (%t, %s), want false with retry", allowed, retry)
	}

	limiter.RecordSuccess("first")
	if allowed, retry := limiter.IsAllowed("second"); !allowed || retry != 0 {
		t.Fatalf("IsAllowed(second after release) = (%t, %s), want true/0", allowed, retry)
	}
}

func TestLoginFailureRateLimiterRecordFailureHonorsCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxIdentities: 1,
		Window:        time.Minute,
		Now:           func() time.Time { return now },
	})

	if got := limiter.RecordFailure("first"); got != 1 {
		t.Fatalf("RecordFailure(first) = %d, want 1", got)
	}
	if got := limiter.RecordFailure("second"); got != 0 {
		t.Fatalf("RecordFailure(second at capacity) = %d, want 0", got)
	}

	limiter.mu.Lock()
	_, hasFirst := limiter.attempts["first"]
	_, hasSecond := limiter.attempts["second"]
	limiter.mu.Unlock()
	if !hasFirst || hasSecond {
		t.Fatalf("attempts after capacity rejection = first:%t second:%t", hasFirst, hasSecond)
	}
}

func TestLoginFailureRateLimiterReservationExpiresAtWindowBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxIdentities: 1,
		Window:        time.Minute,
		Now:           func() time.Time { return now },
	})

	if allowed, _ := limiter.IsAllowed("first"); !allowed {
		t.Fatal("IsAllowed(first) = false")
	}
	now = now.Add(time.Minute)
	if allowed, retry := limiter.IsAllowed("second"); !allowed || retry != 0 {
		t.Fatalf("IsAllowed(second at exact boundary) = (%t, %s), want true/0", allowed, retry)
	}
}

func TestLoginFailureRateLimiterSaturatedMissKeepsExpiryFrontier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
		MaxIdentities: 128,
		Window:        time.Minute,
		Now:           func() time.Time { return now },
	})
	for i := range limiter.maxIdentities {
		if allowed, _ := limiter.IsAllowed(fmt.Sprintf("reserved-%03d", i)); !allowed {
			t.Fatalf("IsAllowed(reserved-%03d) = false", i)
		}
	}

	wantExpiry := now.Add(time.Minute)
	for i := range 1024 {
		if allowed, retry := limiter.IsAllowed(fmt.Sprintf("spray-%04d", i)); allowed || retry <= 0 {
			t.Fatalf("IsAllowed(spray-%04d) = (%t, %s), want false with retry", i, allowed, retry)
		}
	}

	limiter.mu.Lock()
	entryCount := len(limiter.attempts)
	gotExpiry := limiter.nextExpiry
	limiter.mu.Unlock()
	if entryCount != limiter.maxIdentities {
		t.Fatalf("entry count = %d, want %d", entryCount, limiter.maxIdentities)
	}
	if !gotExpiry.Equal(wantExpiry) {
		t.Fatalf("next expiry = %s, want %s", gotExpiry, wantExpiry)
	}
}

func TestLoginFailureRateLimiterConcurrentReservationsHonorCapacity(t *testing.T) {
	t.Parallel()

	const (
		capacity = 8
		callers  = 64
	)
	limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{MaxIdentities: capacity})
	start := make(chan struct{})
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(identity string) {
			defer wg.Done()
			<-start
			if ok, _ := limiter.IsAllowed(identity); ok {
				allowed.Add(1)
			}
		}(fmt.Sprintf("identity-%02d", i))
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != capacity {
		t.Fatalf("allowed reservations = %d, want %d", got, capacity)
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
