package pgxdb

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/stretchr/testify/require"
)

func TestIntegration_AcquirePolicyPreservesInstrumentation(t *testing.T) {
	cfg := mustParse(t, requireContainer(t))

	cfg.MaxConns = 1
	cfg.MinConns = 0

	var mu sync.Mutex

	counts := make(map[string]int)

	var (
		acquireDone     <-chan struct{}
		prematureCancel bool
	)

	cfg.ConnConfig.Tracer = &tracelog.TraceLog{
		LogLevel: tracelog.LogLevelTrace,
		Logger: tracelog.LoggerFunc(func(ctx context.Context, _ tracelog.LogLevel, msg string, _ map[string]any) {
			mu.Lock()
			defer mu.Unlock()

			counts[msg]++
			if msg == "Acquire" {
				acquireDone = ctx.Done()
				prematureCancel = prematureCancel || ctx.Err() != nil
			}
		}),
	}
	require.NoError(t, installAcquirePolicy(cfg, &AcquirePolicy{Timeout: time.Second}))

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	exerciseAcquisitionTraceOperations(t, pool)

	mu.Lock()
	defer mu.Unlock()

	for _, event := range []string{"Connect", "Acquire", "Release", "Query", "CopyFrom", "BatchQuery", "BatchClose", "Prepare"} {
		require.Positive(t, counts[event], "consumer instrumentation lost %s callbacks", event)
	}

	require.False(t, prematureCancel, "instrumentation must observe acquire end before its temporary context is canceled")
	require.NotNil(t, acquireDone)

	select {
	case <-acquireDone:
	default:
		t.Fatal("acquire timer was not disposed after returning")
	}

	require.NoError(t, t.Context().Err(), "acquire completion must not cancel the SQL parent")
}

func exerciseAcquisitionTraceOperations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), "CREATE TEMP TABLE acquire_trace_copy (value int)")
	require.NoError(t, err)

	n, err := pool.CopyFrom(t.Context(), pgx.Identifier{"acquire_trace_copy"}, []string{"value"}, pgx.CopyFromRows([][]any{{1}, {2}}))
	require.NoError(t, err)
	require.EqualValues(t, 2, n)

	batch := &pgx.Batch{}
	batch.Queue("UPDATE acquire_trace_copy SET value = value + 1")

	results := pool.SendBatch(t.Context(), batch)

	_, err = results.Exec()
	require.NoError(t, err)
	require.NoError(t, results.Close())

	var sum int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT sum(value)::int FROM acquire_trace_copy").Scan(&sum))
	require.Equal(t, 5, sum)
	require.NoError(t, pool.AcquireFunc(t.Context(), func(conn *pgxpool.Conn) error {
		_, err := conn.Conn().Prepare(t.Context(), "acquire_trace_statement", "SELECT 42")
		return err
	}))
	require.NoError(t, pool.Ping(t.Context()))
}
