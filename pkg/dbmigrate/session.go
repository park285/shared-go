package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type SessionConfig struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

func (c SessionConfig) Configure(ctx context.Context, exec Execer) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	if c.LockTimeout > 0 {
		if err := exec(ctx, setLockTimeoutSQL, timeoutSetting(c.LockTimeout)); err != nil {
			return fmt.Errorf("dbmigrate: set lock_timeout: %w", err)
		}
	}

	if c.StatementTimeout > 0 {
		if err := exec(ctx, setStatementTimeoutSQL, timeoutSetting(c.StatementTimeout)); err != nil {
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
