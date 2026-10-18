package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

type stageCleanupPool struct {
	pgstore.Querier

	tx *stageCleanupTx
}

func (p stageCleanupPool) Begin(context.Context) (pgx.Tx, error) {
	return p.tx, nil
}

type stageCleanupTx struct {
	pgx.Tx

	cancel         context.CancelFunc
	lockErr        error
	commitErr      error
	rollbackErr    error
	commitCalls    int
	rollbackCalls  int
	rollbackValue  any
	rollbackDone   <-chan struct{}
	rollbackLive   bool
	rollbackBudget time.Duration
}

type stageCleanupContextKey struct{}

func (tx *stageCleanupTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if tx.lockErr != nil {
		tx.cancel()
	}

	return pgconn.CommandTag{}, tx.lockErr
}

func (*stageCleanupTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return stageCleanupRow{}
}

// No rows is the successful ordinal-superseded branch of StageRow. It reaches
// commit without requiring an unrelated stored-reply fixture.
type stageCleanupRow struct{}

func (stageCleanupRow) Scan(...any) error {
	return pgx.ErrNoRows
}

func (tx *stageCleanupTx) Commit(context.Context) error {
	tx.commitCalls++
	tx.cancel()

	return tx.commitErr
}

func (tx *stageCleanupTx) Rollback(ctx context.Context) error {
	tx.rollbackCalls++

	tx.rollbackValue = ctx.Value(stageCleanupContextKey{})
	tx.rollbackDone = ctx.Done()
	tx.rollbackLive = ctx.Err() == nil

	if deadline, ok := ctx.Deadline(); ok {
		tx.rollbackBudget = time.Until(deadline)
	}

	return tx.rollbackErr
}

func TestStagePreservesTransactionErrorsDuringCanceledCleanup(t *testing.T) {
	t.Parallel()

	lockErr := errors.New("lock operation failed")
	commitErr := errors.New("commit response lost")
	rollbackErr := errors.New("cleanup failed")

	for _, tc := range []struct {
		name        string
		lockErr     error
		commitErr   error
		rollbackErr error
		wantErr     error
		wantCleanup bool
	}{
		{"lock failure", lockErr, nil, nil, lockErr, false},
		{"lock and cleanup failure", lockErr, nil, rollbackErr, lockErr, true},
		{"commit outcome unknown", nil, commitErr, pgx.ErrTxClosed, commitErr, false},
		{"commit and cleanup failure", nil, commitErr, rollbackErr, commitErr, true},
		{"committed transaction", nil, nil, fmt.Errorf("closed: %w", pgx.ErrTxClosed), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), stageCleanupContextKey{}, "stage request"))
			defer cancel()

			tx := &stageCleanupTx{cancel: cancel, lockErr: tc.lockErr, commitErr: tc.commitErr, rollbackErr: tc.rollbackErr}

			store, err := pgstore.New(stageCleanupPool{tx: tx}, pgstore.Options{})
			require.NoError(t, err)

			outcome, err := store.Stage(ctx, irisdurable.ReplyRecord{
				MessageID: "cleanup-message", Phase: "reply", RoomID: "cleanup-room",
				ClientRequestID: "cleanup-request", Payload: []byte(`{"text":"test"}`),
			})
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantCleanup, errors.Is(err, rollbackErr))

			if tc.wantErr == nil {
				require.Equal(t, irisdurable.ReplyOrdinalSuperseded, outcome)
			}

			wantCommits := 1

			if tc.lockErr != nil {
				wantCommits = 0
			}

			require.Equal(t, wantCommits, tx.commitCalls)
			require.Equal(t, 1, tx.rollbackCalls)
			require.Error(t, ctx.Err())
			require.True(t, tx.rollbackLive)
			require.Greater(t, tx.rollbackBudget, time.Duration(0))
			require.LessOrEqual(t, tx.rollbackBudget, 5*time.Second)
			require.Equal(t, "stage request", tx.rollbackValue)

			select {
			case <-tx.rollbackDone:
			default:
				t.Fatal("cleanup must release its timer")
			}
		})
	}
}
