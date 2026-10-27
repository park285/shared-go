package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SessionConfig는 migration 세션에 거는 lock_timeout·statement_timeout이다. 0 이하 값은 설정하지 않는다.
// 설정은 set_config(..., false)로 거는 session scope라 connection 수명 동안 유지된다 — exec는
// 마이그레이션 전용 pinned connection이어야 하며, pool 바인딩 exec에 적용하면
// 설정이 임의의 conn 하나에만 남아 이후 세션으로 샌다.
type SessionConfig struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

// Configure는 0보다 큰 timeout을 exec 세션에 설정한다.
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

func timeoutSetting(d time.Duration) string {
	ms := d / time.Millisecond
	if d%time.Millisecond != 0 {
		ms++
	}

	return fmt.Sprintf("%dms", ms)
}
