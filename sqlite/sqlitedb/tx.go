package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrCommit = errors.New("sqlitedb: commit failed")

func (db *DB) WriteTx(ctx context.Context, fn func(*sql.Tx) error) (resultErr error) {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitedb: begin: %w", withContextError(ctx, err))
	}

	committing := false

	defer func() {
		if committing {
			return
		}

		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("sqlitedb: rollback: %w", rollbackErr))
		}
	}()

	if fnErr := fn(tx); fnErr != nil {
		return withContextError(ctx, fnErr)
	}

	committing = true

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("%w: %w", ErrCommit, commitErr)
	}

	return nil
}

func withContextError(ctx context.Context, err error) error {
	ctxErr := ctx.Err()
	if ctxErr == nil || errors.Is(err, ctxErr) {
		return err
	}

	return errors.Join(ctxErr, err)
}
