package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrCommit = errors.New("sqlitedb: commit failed")

type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (db *DB) WriteTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if err := db.gate.acquire(ctx); err != nil {
		return fmt.Errorf("sqlitedb: begin: %w", withContextError(ctx, err))
	}

	defer db.gate.release()

	return runTx(ctx, db.write, fn)
}

func runTx(ctx context.Context, beginner txBeginner, fn func(*sql.Tx) error) (resultErr error) {
	tx, err := beginner.BeginTx(ctx, nil)
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
