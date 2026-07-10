package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// set_config(..., false)는 session scope라 connection 수명 동안 유지된다 — exec는
// 마이그레이션 전용 pinned connection이어야 하며, pool 바인딩 exec에 적용하면
// 설정이 임의의 conn 하나에만 남아 이후 세션으로 샌다.
type SessionConfig struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

func (c SessionConfig) Configure(ctx context.Context, exec Execer) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}
	if c.LockTimeout > 0 {
		if err := exec(ctx, querySetLockTimeout, timeoutSetting(c.LockTimeout)); err != nil {
			return fmt.Errorf("dbmigrate: set lock_timeout: %w", err)
		}
	}
	if c.StatementTimeout > 0 {
		if err := exec(ctx, querySetStatementTimeout, timeoutSetting(c.StatementTimeout)); err != nil {
			return fmt.Errorf("dbmigrate: set statement_timeout: %w", err)
		}
	}
	return nil
}

func WithSession(cfg SessionConfig) Option {
	return func(o *options) {
		o.session = &cfg
	}
}

func timeoutSetting(d time.Duration) string {
	ms := d / time.Millisecond
	if d%time.Millisecond != 0 {
		ms++
	}
	return fmt.Sprintf("%dms", ms)
}
