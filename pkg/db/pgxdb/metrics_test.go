package pgxdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestIntegrationServerCancellationPreservesSQLStateAndConnection(t *testing.T) {
	dsn := requireContainer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	pool, err := OpenPoolDSN(ctx, dsn+"&application_name=dsn-name", Options{
		Logger: quietLogger(), Pool: PoolConfig{MaxConns: 1, PingTimeout: time.Second},
		RuntimeParams: map[string]string{"application_name": "shared-cancel-test", "statement_timeout": "10s"},
		CancelRequest: &CancelRequestConfig{DeadlineDelay: time.Second},
	})
	require.NoError(t, err)

	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	pid := conn.Conn().PgConn().PID()

	var name, timeout string

	require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('application_name'), current_setting('statement_timeout')").Scan(&name, &timeout))
	require.Equal(t, "shared-cancel-test", name)
	require.Equal(t, "10s", timeout)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, Rollback(ctx, tx)) }()

	attemptCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)

	defer stop()

	_, err = tx.Exec(attemptCtx, "SELECT pg_sleep(5)")

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "server SQLSTATE must remain visible: %v", err)
	require.Equal(t, "57014", pgErr.Code)
	require.NoError(t, Rollback(attemptCtx, tx))
	require.Equal(t, pid, conn.Conn().PgConn().PID())
	require.NoError(t, selectOne(ctx, conn))

	stat := pool.Stat()

	var acquired float64

	VisitPoolMetrics(stat, func(metric PoolMetric) {
		if metric.Suffix == "conns" && metric.State == "acquired" {
			acquired = metric.Value
		}
	})
	require.InDelta(t, 1, acquired, 0)
}
