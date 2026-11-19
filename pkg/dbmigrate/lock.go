package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultLockAcquire = 30 * time.Second
	defaultLockPoll    = 25 * time.Millisecond
	defaultLockRelease = 5 * time.Second
)

type LockSession interface {
	TryAdvisoryLock(ctx context.Context, key int64) (bool, error)
	AdvisoryUnlock(ctx context.Context, key int64) (bool, error)
}

type UnlockFailureEvicter interface {
	EvictAfterUnlockFailure() error
}

type LockConfig struct {
	Key           int64
	Acquire       time.Duration
	Poll          time.Duration
	Release       time.Duration
	OnUnlockError func(error)
}

func WithAdvisoryLock(ctx context.Context, s LockSession, cfg LockConfig, fn func(context.Context) error) (err error) {
	if s == nil {
		return errors.New("dbmigrate: lock session is required")
	}

	if fn == nil {
		return errors.New("dbmigrate: lock function is required")
	}

	cfg = cfg.withDefaults()
	if acquireErr := acquireAdvisoryLock(ctx, s, cfg); acquireErr != nil {
		return acquireErr
	}

	defer func() {
		unlockErr := releaseAdvisoryLock(ctx, s, cfg)
		if unlockErr == nil {
			return
		}

		if evicter, ok := s.(UnlockFailureEvicter); ok {
			if evictErr := evictAfterUnlockFailure(evicter); evictErr != nil {
				unlockErr = errors.Join(unlockErr, evictErr)
			}
		}

		if cfg.OnUnlockError != nil {
			cfg.OnUnlockError(unlockErr)

			return
		}

		if err != nil {
			err = errors.Join(err, unlockErr)
			return
		}

		err = unlockErr
	}()

	return fn(ctx)
}

func evictAfterUnlockFailure(e UnlockFailureEvicter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dbmigrate: evict conn after unlock failure panicked: %v", r)
		}
	}()

	if err := e.EvictAfterUnlockFailure(); err != nil {
		return fmt.Errorf("evict after unlock failure: %w", err)
	}

	return nil
}

func (c LockConfig) withDefaults() LockConfig {
	if c.Acquire <= 0 {
		c.Acquire = defaultLockAcquire
	}

	if c.Poll <= 0 {
		c.Poll = defaultLockPoll
	}

	if c.Release <= 0 {
		c.Release = defaultLockRelease
	}

	return c
}

func acquireAdvisoryLock(ctx context.Context, s LockSession, cfg LockConfig) error {
	lockCtx, cancel := context.WithTimeout(ctx, cfg.Acquire)
	defer cancel()

	ticker := time.NewTicker(cfg.Poll)
	defer ticker.Stop()

	for {
		acquired, err := s.TryAdvisoryLock(lockCtx, cfg.Key)
		if err != nil {
			return fmt.Errorf("dbmigrate: try acquire migration advisory lock: %w", err)
		}

		if acquired {
			return nil
		}

		select {
		case <-lockCtx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return fmt.Errorf("dbmigrate: wait for migration advisory lock canceled: %w", ctx.Err())
			}

			return fmt.Errorf("dbmigrate: wait for migration advisory lock timed out after %s: %w", cfg.Acquire, lockCtx.Err())
		case <-ticker.C:
		}
	}
}

func releaseAdvisoryLock(ctx context.Context, s LockSession, cfg LockConfig) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Release)
	defer cancel()

	released, err := s.AdvisoryUnlock(releaseCtx, cfg.Key)
	if err != nil {
		return fmt.Errorf("dbmigrate: release migration advisory lock: %w", err)
	}

	if !released {
		return errors.New("dbmigrate: release migration advisory lock: lock was not held by session")
	}

	return nil
}
