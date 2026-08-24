package pgxdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Rollbacker interface {
	Rollback(ctx context.Context) error
}

//nolint:gocritic // defer에서 호출자의 named return error를 갱신해야 하므로 포인터가 필수다.
func RollbackDeferred(ctx context.Context, tx Rollbacker, err *error) {
	rollbackErr := tx.Rollback(ctx)
	if rollbackErr == nil || errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return
	}

	*err = errors.Join(*err, fmt.Errorf("rollback transaction: %w", rollbackErr))
}
