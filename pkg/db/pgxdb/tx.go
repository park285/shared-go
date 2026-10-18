package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Rollbacker interface {
	Rollback(ctx context.Context) error
}

// Rollback releases a transaction independently of caller cancellation, with a
// five-second cleanup deadline. Already committed or rolled-back transactions
// are successful no-ops; all other rollback errors retain their wrapped cause.
func Rollback(ctx context.Context, tx Rollbacker) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	err := tx.Rollback(cleanupCtx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}

	return nil
}

//nolint:gocritic // defer에서 호출자의 named return error를 갱신해야 하므로 포인터가 필수다.
func RollbackDeferred(ctx context.Context, tx Rollbacker, err *error) {
	rollbackErr := Rollback(ctx, tx)
	if rollbackErr == nil {
		return
	}

	*err = errors.Join(*err, fmt.Errorf("rollback transaction: %w", rollbackErr))
}
