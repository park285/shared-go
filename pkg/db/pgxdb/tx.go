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

// Rollback은 호출자 취소와 독립된 5초 기한으로 트랜잭션을 정리한다.
// 이미 커밋·롤백된 트랜잭션은 성공으로 처리하고 다른 오류의 원인은 보존한다.
func Rollback(ctx context.Context, tx Rollbacker) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	err := tx.Rollback(cleanupCtx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("pgxdb: rollback: %w", err)
	}

	return nil
}

//nolint:gocritic // defer에서 호출자의 named return error를 갱신해야 하므로 포인터가 필수다.
func RollbackDeferred(ctx context.Context, tx Rollbacker, err *error) {
	rollbackErr := Rollback(ctx, tx)
	if rollbackErr == nil {
		return
	}

	*err = errors.Join(*err, rollbackErr)
}
