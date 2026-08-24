package httputil

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultLoginFailureMaxIdentities = 10000

// LoginFailureRateLimiterOptions는 로그인 실패 lockout limiter 설정이다. MaxIdentities의 zero value는 10,000이며, 상한에 도달하면 새 identity를 거부한다.
type LoginFailureRateLimiterOptions struct {
	MaxAttempts     int
	Window          time.Duration
	Lockout         time.Duration
	CleanupInterval time.Duration
	MaxIdentities   int
	Now             func() time.Time
}

// LoginFailureRateLimiter는 로그인 실패 횟수 기반 lockout limiter다.
type LoginFailureRateLimiter struct {
	mu            sync.Mutex
	attempts      map[string]loginAttemptInfo
	maxAttempts   int
	window        time.Duration
	lockout       time.Duration
	cleanup       time.Duration
	maxIdentities int
	now           func() time.Time
	nextExpiry    time.Time
	stop          chan struct{}
	done          chan struct{}
	started       atomic.Bool
	startOnce     sync.Once
	stopOnce      sync.Once
}

type loginAttemptInfo struct {
	count        int
	firstAttempt time.Time
	lockedUntil  time.Time
}

// NewLoginFailureRateLimiter는 로그인 실패 기반 lockout limiter를 생성한다.
func NewLoginFailureRateLimiter(opts LoginFailureRateLimiterOptions) *LoginFailureRateLimiter {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}

	if opts.Window <= 0 {
		opts.Window = 5 * time.Minute
	}

	if opts.Lockout <= 0 {
		opts.Lockout = 15 * time.Minute
	}

	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = time.Minute
	}

	if opts.MaxIdentities <= 0 {
		opts.MaxIdentities = defaultLoginFailureMaxIdentities
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &LoginFailureRateLimiter{
		attempts:      make(map[string]loginAttemptInfo),
		maxAttempts:   opts.MaxAttempts,
		window:        opts.Window,
		lockout:       opts.Lockout,
		cleanup:       opts.CleanupInterval,
		maxIdentities: opts.MaxIdentities,
		now:           opts.Now,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
}

// NewDefaultLoginFailureRateLimiter는 admin-dashboard와 같은 기본 lockout 값을 사용한다.
func NewDefaultLoginFailureRateLimiter() *LoginFailureRateLimiter {
	return NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{})
}

// Start는 stale attempt cleanup loop를 시작한다.
func (l *LoginFailureRateLimiter) Start() {
	if l == nil {
		return
	}

	l.startOnce.Do(func() {
		l.started.Store(true)

		go l.cleanupLoop()
	})
}

// Stop은 cleanup loop를 정지한다.
func (l *LoginFailureRateLimiter) Stop() {
	if l == nil {
		return
	}

	l.stopOnce.Do(func() {
		close(l.stop)
	})

	if !l.started.Load() {
		return
	}

	<-l.done
}

// IsAllowed는 identity가 현재 로그인 시도를 할 수 있는지와 retry-after를 반환한다.
func (l *LoginFailureRateLimiter) IsAllowed(identity string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}

	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	info, ok := l.attempts[identity]

	if !ok {
		if !l.hasCapacityLocked(now) {
			return false, l.cleanup
		}

		info = loginAttemptInfo{firstAttempt: now}
		l.attempts[identity] = info
		l.scheduleExpiryLocked(info)

		return true, 0
	}

	if !info.lockedUntil.IsZero() {
		if now.Before(info.lockedUntil) {
			return false, info.lockedUntil.Sub(now)
		}

		info = loginAttemptInfo{firstAttempt: now}
		l.attempts[identity] = info
		l.scheduleExpiryLocked(info)

		return true, 0
	}

	if now.Sub(info.firstAttempt) >= l.window {
		info = loginAttemptInfo{firstAttempt: now}
		l.attempts[identity] = info
		l.scheduleExpiryLocked(info)

		return true, 0
	}

	return info.count < l.maxAttempts, 0
}

// RecordFailure는 identity의 실패 횟수를 기록하고 현재 window count를 반환한다.
func (l *LoginFailureRateLimiter) RecordFailure(identity string) int {
	if l == nil {
		return 0
	}

	identity = strings.TrimSpace(identity)
	if identity == "" {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	info, ok := l.attempts[identity]

	if !ok {
		if !l.hasCapacityLocked(now) {
			return 0
		}

		info = loginAttemptInfo{firstAttempt: now}
	} else if info.firstAttempt.IsZero() ||
		(!info.lockedUntil.IsZero() && !now.Before(info.lockedUntil)) ||
		now.Sub(info.firstAttempt) >= l.window {
		info = loginAttemptInfo{firstAttempt: now}
	}

	info.count++
	if info.count >= l.maxAttempts {
		info.lockedUntil = now.Add(l.lockout)
	}

	l.attempts[identity] = info
	l.scheduleExpiryLocked(info)

	return info.count
}

// RecordSuccess는 identity의 실패 기록을 삭제한다.
func (l *LoginFailureRateLimiter) RecordSuccess(identity string) {
	if l == nil {
		return
	}

	identity = strings.TrimSpace(identity)
	if identity == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, identity)
}

func (l *LoginFailureRateLimiter) cleanupLoop() {
	defer close(l.done)

	ticker := time.NewTicker(l.cleanup)

	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.cleanupStale(l.now())
		}
	}
}

func (l *LoginFailureRateLimiter) cleanupStale(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupStaleLocked(now)
}

func (l *LoginFailureRateLimiter) cleanupStaleLocked(now time.Time) {
	l.nextExpiry = time.Time{}
	for identity, info := range l.attempts {
		if !info.lockedUntil.IsZero() {
			if !now.Before(info.lockedUntil) {
				delete(l.attempts, identity)

				continue
			}
		} else if now.Sub(info.firstAttempt) >= l.window {
			delete(l.attempts, identity)

			continue
		}

		l.scheduleExpiryLocked(info)
	}
}

func (l *LoginFailureRateLimiter) hasCapacityLocked(now time.Time) bool {
	if len(l.attempts) < l.maxIdentities {
		return true
	}

	l.cleanupStaleIfDueLocked(now)

	return len(l.attempts) < l.maxIdentities
}

func (l *LoginFailureRateLimiter) cleanupStaleIfDueLocked(now time.Time) {
	if l.nextExpiry.IsZero() || !now.Before(l.nextExpiry) {
		l.cleanupStaleLocked(now)
	}
}

func (l *LoginFailureRateLimiter) scheduleExpiryLocked(info loginAttemptInfo) {
	expiresAt := info.lockedUntil
	if expiresAt.IsZero() {
		expiresAt = info.firstAttempt.Add(l.window)
	}

	if l.nextExpiry.IsZero() || expiresAt.Before(l.nextExpiry) {
		l.nextExpiry = expiresAt
	}
}
