package dbmigrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

const (
	defaultLockAcquire = 30 * time.Second
	defaultLockPoll    = 25 * time.Millisecond
	defaultLockRelease = 5 * time.Second
)

// LockSession은 PostgreSQL advisory lock 최소 동작이다.
type LockSession interface {
	// TryAdvisoryLock은 advisory lock 획득을 한 번 시도한다.
	TryAdvisoryLock(ctx context.Context, key int64) (bool, error)
	// AdvisoryUnlock은 advisory lock 해제를 시도한다.
	AdvisoryUnlock(ctx context.Context, key int64) (bool, error)
}

// WithAdvisoryLock은 unlock이 에러이거나 released=false(lock 미보유)로 끝나면 세션 lock
// 상태가 불확실하다고 보고 이 메서드를 호출한다. pooled conn 구현체는 여기서 conn을 hijack 후
// close해, lock을 쥔 채 풀로 반환·재사용되는 것을 막아야 한다.
type UnlockFailureEvicter interface {
	EvictAfterUnlockFailure() error
}

// LockConfig는 migration advisory lock 획득과 해제 설정이다.
type LockConfig struct {
	// Key는 PostgreSQL advisory lock key다.
	Key int64
	// Acquire는 lock 획득 최대 대기 시간이다.
	Acquire time.Duration
	// Poll은 lock 획득 재시도 간격이다.
	Poll time.Duration
	// Release는 lock 해제 최대 대기 시간이다.
	Release time.Duration
	// OnUnlockError는 unlock 실패를 return 대신 전달받는 callback이다.
	OnUnlockError func(error)
}

// WithAdvisoryLock은 advisory lock을 잡은 동안 fn을 실행한다.
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

// fn의 panic unwinding 중 defer 안에서 evict가 다시 panic하면 원 panic을 대체하고
// OnUnlockError·errors.Join을 건너뛴다 — 오류로 변환해 unlock 오류 의미를 보존한다.
func evictAfterUnlockFailure(e UnlockFailureEvicter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dbmigrate: evict conn after unlock failure panicked: %v", r)
		}
	}()
	return e.EvictAfterUnlockFailure()
}

// SQLLockSession은 database/sql 연결을 LockSession으로 감싼다.
func SQLLockSession(c *sql.Conn) LockSession {
	return sqlLockSession{conn: c}
}

type sqlLockSession struct {
	conn *sql.Conn
}

func (s sqlLockSession) EvictAfterUnlockFailure() error {
	if s.conn == nil {
		return errors.New("dbmigrate: sql lock connection is nil")
	}
	err := s.conn.Raw(func(any) error {
		return driver.ErrBadConn
	})
	if err != nil && !errors.Is(err, driver.ErrBadConn) {
		return fmt.Errorf("dbmigrate: evict sql lock connection: %w", err)
	}
	return nil
}

func (s sqlLockSession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	if s.conn == nil {
		return false, errors.New("dbmigrate: sql lock connection is nil")
	}
	var acquired bool
	if err := s.conn.QueryRowContext(ctx, queryTryAdvisoryLock, key).Scan(&acquired); err != nil {
		return false, err
	}
	return acquired, nil
}

func (s sqlLockSession) AdvisoryUnlock(ctx context.Context, key int64) (bool, error) {
	if s.conn == nil {
		return false, errors.New("dbmigrate: sql lock connection is nil")
	}
	var released bool
	if err := s.conn.QueryRowContext(ctx, queryAdvisoryUnlock, key).Scan(&released); err != nil {
		return false, err
	}
	return released, nil
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
