package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

func retireAndCommit(t *testing.T, pool *pgxpool.Pool, opts pgstore.Options) (retired []pgstore.RetiredReply, resultErr error) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		return nil, fmt.Errorf("begin retirement: %w", err)
	}
	defer pgxdb.RollbackDeferred(t.Context(), tx, &resultErr)

	store, err := pgstore.New(tx, opts)
	if err != nil {
		return nil, fmt.Errorf("create transaction store: %w", err)
	}

	retired, err = store.Retire(t.Context(), 10)
	if err != nil {
		return nil, fmt.Errorf("retire replies: %w", err)
	}

	if err := tx.Commit(t.Context()); err != nil {
		return nil, fmt.Errorf("commit retirement: %w", err)
	}

	return retired, nil
}

func TestRetireRequiresTransactionBeforeQuery(t *testing.T) {
	// Calling any method on the embedded nil Querier panics. Rejection must not
	// query or implicitly begin a transaction, even for a valid limit.
	store, err := pgstore.New(struct{ pgstore.Querier }{}, pgstore.Options{})
	require.NoError(t, err)

	for _, limit := range []int{0, 1} {
		rows, err := store.Retire(t.Context(), limit)
		require.Error(t, err)
		require.Nil(t, rows)
	}
}

type retirementFaultTx struct {
	pgx.Tx

	readErr     error
	scanFailure bool
}

func (tx retirementFaultTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	return retirementFaultRows{Rows: rows, readErr: tx.readErr, scanFailure: tx.scanFailure}, nil
}

type retirementFaultRows struct {
	pgx.Rows

	readErr     error
	scanFailure bool
}

func (r retirementFaultRows) Scan(dest ...any) error {
	if r.scanFailure {
		// Exercise pgx's real conversion failure after UPDATE RETURNING rather
		// than a SQL failure (a scan error need not abort the transaction).
		var invalidPayload int64

		dest[6] = &invalidPayload
	}

	return r.Rows.Scan(dest...)
}

func (r retirementFaultRows) Err() error {
	return errors.Join(r.Rows.Err(), r.readErr)
}

func TestRetirePayloadFailureRollsBackWithoutPoolMutation(t *testing.T) {
	pool := newMigratedPool(t)

	for _, scanFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "scan", false: "read"}[scanFailure], func(t *testing.T) {
			store := newScopedStore(t, pool)
			record := (&replyFixture{Store: store}).NewRecord(t, []byte(`{"text":"must survive retirement failure"}`))
			_, err := store.Stage(t.Context(), record)
			require.NoError(t, err)

			_, err = pool.Exec(t.Context(), "UPDATE iris_reply_outbox SET attempts = $2 WHERE scope = $1", store.Options().Scope, store.Options().MaxAttempts)
			require.NoError(t, err)

			rows, err := store.Retire(t.Context(), 10)
			require.Error(t, err)
			require.Nil(t, rows)

			before, err := store.Inspect(t.Context(), record.ReplyIdentity)
			require.NoError(t, err)
			require.Equal(t, irisdurable.ReplyStatusPending, before.Status)
			require.True(t, before.PayloadPresent)

			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

			fault := retirementFaultTx{Tx: tx, scanFailure: scanFailure}
			if !scanFailure {
				fault.readErr = errors.New("retirement result stream lost")
			}

			transactionStore, err := pgstore.New(fault, store.Options())
			require.NoError(t, err)

			rows, err = transactionStore.Retire(t.Context(), 10)
			require.Error(t, err)
			require.Nil(t, rows)
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))

			after, err := store.Inspect(t.Context(), record.ReplyIdentity)
			require.NoError(t, err)
			require.Equal(t, before, after)

			var payload string

			require.NoError(t, pool.QueryRow(t.Context(), "SELECT payload::text FROM iris_reply_outbox WHERE scope = $1", store.Options().Scope).Scan(&payload))
			require.JSONEq(t, string(record.Payload), payload)
		})
	}
}
