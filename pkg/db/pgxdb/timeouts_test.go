package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func closeTestSocket(t *testing.T, socket io.Closer) {
	t.Helper()

	if err := socket.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("close test socket: %v", err)
	}
}

func TestStartupMinimumPoolBlackholeIsBounded(t *testing.T) {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var workers sync.WaitGroup

	workers.Go(func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			workers.Go(func() {
				defer closeTestSocket(t, conn)

				// Test watchdog bounds cleanup even if the client clamp regresses.
				if deadlineErr := conn.SetDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
					t.Errorf("set test connection deadline: %v", deadlineErr)

					return
				}

				if _, copyErr := io.Copy(io.Discard, conn); copyErr != nil &&
					!errors.Is(copyErr, os.ErrDeadlineExceeded) && !errors.Is(copyErr, net.ErrClosed) {
					t.Errorf("read blackholed test connection: %v", copyErr)
				}
			})
		}
	})

	defer func() {
		closeTestSocket(t, listener)

		workers.Wait()
	}()

	started := time.Now()
	pool, err := OpenPoolDSN(t.Context(), "postgres://test@"+listener.Addr().String()+"/test?sslmode=disable&pool_min_conns=1&pool_max_conns=1&connect_timeout=30", Options{
		Logger: quietLogger(), Ping: PingConfig{PingTimeout: 100 * time.Millisecond},
	})
	require.Error(t, err)
	require.Nil(t, pool)
	require.Less(t, time.Since(started), time.Second, "startup failure cleanup must not wait for the original connect timeout")
}

type blackholeWriteConn struct {
	net.Conn

	drop atomic.Bool
}

func (c *blackholeWriteConn) Write(data []byte) (int, error) {
	if c.drop.Load() {
		return len(data), nil
	}

	written, err := c.Conn.Write(data)
	if err != nil {
		return written, fmt.Errorf("forward test connection write: %w", err)
	}

	return written, nil
}

func TestIntegrationAcquireHealthPingBoundsHalfOpenIdleConnection(t *testing.T) {
	probeFailed, pool, old := newHalfOpenHealthPool(t)

	defer closeTestSocket(t, old)

	started := time.Now()
	acquired := make(chan error, 1)

	go func() {
		//nolint:usetesting // The policy, not a caller deadline, must bound acquisition.
		conn, acquireErr := pool.Acquire(context.Background())
		if conn != nil {
			conn.Release()
		}

		acquired <- acquireErr
	}()

	select {
	case <-probeFailed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("health ping did not close the half-open connection within its budget")
	}

	// pgx retains the failed slot during its independent cleanup grace period.
	// The acquire deadline must expire without waiting for that cleanup.
	select {
	case acquireErr := <-acquired:
		require.ErrorIs(t, acquireErr, context.DeadlineExceeded)
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("acquisition exceeded its total budget")
	}

	require.Less(t, time.Since(started), 2*time.Second)

	closeTestSocket(t, old)

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer conn.Release()

	require.NoError(t, selectOne(t.Context(), conn))
	require.GreaterOrEqual(t, pool.Stat().NewConnsCount(), int64(2))
}

func newHalfOpenHealthPool(t *testing.T) (chan struct{}, *pgxpool.Pool, *blackholeWriteConn) {
	t.Helper()

	dsn := requireContainer(t)
	cfg := mustParse(t, dsn)
	require.NoError(t, overlayPoolConfig(cfg, PoolConfig{MaxConns: 1, PingTimeout: 100 * time.Millisecond}))

	cfg.ShouldPing = func(context.Context, pgxpool.ShouldPingParams) bool { return true }
	require.NoError(t, installAcquirePolicy(cfg, &AcquirePolicy{Timeout: time.Second}))

	probeFailed := make(chan struct{})

	var closedOnce sync.Once

	cfg.BeforeClose = func(conn *pgx.Conn) {
		if conn.IsClosed() {
			closedOnce.Do(func() { close(probeFailed) })
		}
	}

	var (
		first  atomic.Pointer[blackholeWriteConn]
		dialer net.Dialer
	)

	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}

		wrapped := &blackholeWriteConn{Conn: conn}
		first.CompareAndSwap(nil, wrapped)

		return wrapped, nil
	}

	pool, err := newPoolAndPing(t.Context(), cfg, quietOptions())
	require.NoError(t, err)

	t.Cleanup(pool.Close)

	old := first.Load()
	require.NotNil(t, old)
	old.drop.Store(true)

	return probeFailed, pool, old
}
