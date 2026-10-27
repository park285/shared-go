package pgxdb

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func policyTestPool(t *testing.T, maxConns int, policy *AcquirePolicy) *pgxpool.Pool {
	t.Helper()

	pool, err := OpenPoolDSN(t.Context(), requireContainer(t), Options{
		Logger: quietLogger(), Pool: PoolConfig{MaxConns: maxConns}, Ping: testPing(), AcquirePolicy: policy,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func TestIntegration_AcquirePolicyControlContention(t *testing.T) {
	policy := &AcquirePolicy{ReservedConns: 1, Timeout: time.Second}
	pool := policyTestPool(t, 2, policy)
	// 생성 후 원본 변경이 예약 용량·기한에 영향을 주어서는 안 된다.
	*policy = AcquirePolicy{ReservedConns: 0, Timeout: time.Nanosecond}

	ordinary, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer ordinary.Release()

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	queued := make(chan error, 1)

	go func() { queued <- selectOne(ctx, pool) }()

	select {
	case queuedErr := <-queued:
		t.Fatalf("ordinary work consumed reserved capacity: %v", queuedErr)
	case <-time.After(30 * time.Millisecond):
	}

	controlCtx := WithControlAcquire(t.Context())
	// 일반 작업의 허가가 소진되고 대기열이 생겨도 임대 갱신·정산 SQL은 실행 가능해야 한다.
	_, err = pool.Exec(controlCtx, "CREATE TEMP TABLE acquire_lease (id int, generation int)")
	require.NoError(t, err)

	_, err = pool.Exec(controlCtx, "INSERT INTO acquire_lease VALUES (1, 1)")
	require.NoError(t, err)

	var generation int

	err = pool.QueryRow(controlCtx, "UPDATE acquire_lease SET generation = generation + 1 WHERE id = 1 RETURNING generation").Scan(&generation)
	require.NoError(t, err)
	require.Equal(t, 2, generation)
	require.ErrorIs(t, <-queued, context.DeadlineExceeded)
	ordinary.Release()
	ordinary.Release() // 멱등적인 Release가 허가를 두 번 반환해서는 안 된다.
	require.NoError(t, selectOne(t.Context(), pool))
	require.NoError(t, selectOne(controlCtx, pool))
}

func TestIntegration_AcquirePolicyLeaseLifetimes(t *testing.T) {
	for _, kind := range []string{"connection", "rows", "row", "commit", "rollback", "batch"} {
		t.Run(kind, func(t *testing.T) {
			pool := policyTestPool(t, 2, &AcquirePolicy{ReservedConns: 1, Timeout: 200 * time.Millisecond})
			ctx := t.Context()

			var release func()

			switch kind {
			case "connection":
				conn, err := pool.Acquire(ctx)
				require.NoError(t, err)

				release = conn.Release
			case "rows":
				rows, err := pool.Query(ctx, "SELECT generate_series(1, 3)")
				require.NoError(t, err)

				defer rows.Close()

				release = rows.Close
			case "row":
				row := pool.QueryRow(ctx, "SELECT 1")

				release = func() {
					var n int

					require.NoError(t, row.Scan(&n))
					require.Equal(t, 1, n)
				}
			case "commit", "rollback":
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)

				release = func() {
					if kind == "commit" {
						require.NoError(t, tx.Commit(ctx))
					} else {
						require.NoError(t, Rollback(ctx, tx))
					}
				}
			case "batch":
				batch := &pgx.Batch{}
				batch.Queue("SELECT 1")

				results := pool.SendBatch(ctx, batch)

				release = func() { require.NoError(t, results.Close()) }
			}

			// 검증 실패 시에도 Close가 멈추지 않도록 정리를 먼저 등록한다.
			onceRelease := sync.OnceFunc(release)
			t.Cleanup(onceRelease)
			require.NoError(t, selectOne(WithControlAcquire(ctx), pool))
			require.ErrorIs(t, selectOne(ctx, pool), context.DeadlineExceeded)
			onceRelease()
			require.NoError(t, selectOne(ctx, pool))
		})
	}
}

func TestIntegration_AcquirePolicyDoesNotBoundSQL(t *testing.T) {
	pool := policyTestPool(t, 1, &AcquirePolicy{Timeout: 100 * time.Millisecond})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)

	defer cancel()

	_, err := pool.Exec(ctx, "SELECT pg_sleep(0.25)")
	require.NoError(t, err, "acquire timeout must end before query execution")

	// SQL 오류를 획득 오류로 바꾸지 않는다.
	_, err = pool.Exec(ctx, "SELECT 1/0")

	var sqlState interface{ SQLState() string }

	require.ErrorAs(t, err, &sqlState)
	require.Equal(t, "22012", sqlState.SQLState())
	require.NoError(t, selectOne(ctx, pool))
}

func TestIntegration_AcquirePolicyControlStillObeysPoolLimit(t *testing.T) {
	pool := policyTestPool(t, 2, &AcquirePolicy{ReservedConns: 1, Timeout: 150 * time.Millisecond})
	ctx := WithControlAcquire(t.Context())
	first, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer first.Release()

	second, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer second.Release()

	_, err = pool.Acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	_, err = pool.Acquire(t.Context()) // 허가는 얻지만 실제 풀 대기는 실패한다.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	second.Release()
	require.NoError(t, selectOne(t.Context(), pool), "failed physical acquire must restore its ordinary permit")
}

func TestIntegration_AcquirePolicyCancellationReuseAndReset(t *testing.T) {
	pool := policyTestPool(t, 4, &AcquirePolicy{ReservedConns: 1, Timeout: time.Second})

	for range 8 {
		held := make([]*pgxpool.Conn, 0, 3)

		t.Cleanup(func() {
			for _, conn := range held {
				conn.Release()
			}
		})

		for range 3 {
			conn, err := pool.Acquire(t.Context())
			require.NoError(t, err)

			held = append(held, conn)
		}

		ctx, cancel := context.WithCancel(t.Context())

		var wg sync.WaitGroup

		errs := make(chan error, 32)

		for range 32 {
			wg.Go(func() {
				conn, err := pool.Acquire(ctx)
				if err == nil {
					conn.Release()
				} else if !errors.Is(err, context.Canceled) {
					errs <- err
				}
			})
		}

		wg.Go(cancel)
		wg.Go(pool.Reset)

		for _, conn := range held {
			wg.Go(conn.Release)
		}

		wg.Wait()

		held = nil

		close(errs)

		for err := range errs {
			t.Errorf("acquire cancellation race: %v", err)
		}
	}

	// 쿼리 한 건 성공이 아닌 일반 허가 전체의 복구를 확인한다.
	restored := make([]*pgxpool.Conn, 0, 3)

	defer func() {
		for _, conn := range restored {
			conn.Release()
		}
	}()

	for range 3 {
		conn, err := pool.Acquire(t.Context())
		require.NoError(t, err)

		restored = append(restored, conn)
	}

	require.NoError(t, selectOne(WithControlAcquire(t.Context()), pool))
}

func TestIntegration_AcquirePolicyHealthAndPrepareFailures(t *testing.T) {
	cfg := mustParse(t, requireContainer(t))

	cfg.MaxConns = 2
	cfg.MinConns = 0

	var breakPing atomic.Bool

	cfg.ShouldPing = func(ctx context.Context, params pgxpool.ShouldPingParams) bool {
		if breakPing.CompareAndSwap(true, false) {
			require.NoError(t, params.Conn.Close(ctx))
		}

		return true
	}

	var reject atomic.Bool

	prepareErr := errors.New("connection preparation rejected")

	cfg.PrepareConn = func(context.Context, *pgx.Conn) (bool, error) {
		if reject.CompareAndSwap(true, false) {
			return true, prepareErr
		}

		return true, nil
	}
	require.NoError(t, installAcquirePolicy(cfg, &AcquirePolicy{ReservedConns: 1, Timeout: time.Second}))

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, selectOne(t.Context(), pool))
	breakPing.Store(true)
	require.NoError(t, selectOne(t.Context(), pool), "native failed health ping must replace connection without losing permit")
	reject.Store(true)
	require.ErrorIs(t, selectOne(t.Context(), pool), prepareErr)
	require.NoError(t, selectOne(t.Context(), pool), "prepare error must restore permit even without a physical release trace")

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer conn.Release()

	require.NoError(t, conn.Conn().Close(t.Context()))
	conn.Release()
	require.NoError(t, selectOne(t.Context(), pool), "destroyed released connection must restore permit")
	pool.Close()

	_, err = pool.Acquire(t.Context())
	require.Error(t, err)

	_, err = pool.Acquire(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded, "closed pool must not leak its ordinary permit")
}

func TestIntegration_AcquirePolicyConstructorFailureAndTimeout(t *testing.T) {
	for _, mode := range []string{"failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			cfg := mustParse(t, requireContainer(t))

			cfg.MaxConns = 2
			cfg.MinConns = 0

			var first atomic.Bool

			first.Store(true)

			unblock := make(chan struct{})
			unblockOnce := sync.OnceFunc(func() { close(unblock) })
			connectErr := errors.New("after-connect rejected")

			cfg.AfterConnect = func(context.Context, *pgx.Conn) error {
				if first.CompareAndSwap(true, false) {
					if mode == "timeout" {
						<-unblock

						return nil
					}

					return connectErr
				}

				return nil
			}
			require.NoError(t, installAcquirePolicy(cfg, &AcquirePolicy{ReservedConns: 1, Timeout: 250 * time.Millisecond}))

			pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			t.Cleanup(unblockOnce)

			conn, err := pool.Acquire(t.Context())
			if conn != nil {
				conn.Release()
			}

			if mode == "timeout" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.ErrorIs(t, err, connectErr)
			}

			// Puddle의 지연된 생성이 획득 기한 뒤에 끝나도 유휴 연결에 허가가 누수되어서는 안 된다.
			unblockOnce()
			require.NoError(t, selectOne(t.Context(), pool))
			require.NoError(t, selectOne(WithControlAcquire(t.Context()), pool))
		})
	}
}

func TestIntegration_AcquirePolicyOneBudgetAcrossGateAndPool(t *testing.T) {
	cfg := mustParse(t, requireContainer(t))

	cfg.MaxConns = 2
	cfg.MinConns = 0

	releasePhysical := make(chan struct{})

	cfg.AfterRelease = func(*pgx.Conn) bool {
		<-releasePhysical

		return true
	}
	require.NoError(t, installAcquirePolicy(cfg, &AcquirePolicy{ReservedConns: 1, Timeout: 800 * time.Millisecond}))

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	t.Cleanup(func() { close(releasePhysical) })

	ordinary, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer ordinary.Release()

	control, err := pool.Acquire(WithControlAcquire(t.Context()))
	require.NoError(t, err)

	defer control.Release()

	started := time.Now()
	result := make(chan error, 1)

	go func() {
		conn, err := pool.Acquire(t.Context())
		if conn != nil {
			conn.Release()
		}

		result <- err
	}()

	// 허가 반환 뒤 AfterRelease가 물리 연결 반환을 지연해 실제 풀에서도 대기하게 한다.
	<-time.After(500 * time.Millisecond)
	ordinary.Release()
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 1100*time.Millisecond, "pool wait must use the remaining budget, not restart it")
}
