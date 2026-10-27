package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

var testDSN string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	if !dockerAvailable() {
		return m.Run()
	}

	id, dsn, err := startPostgres()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgxdb: postgres container unavailable, integration tests will skip:", err)

		return m.Run()
	}
	defer removeContainer(id)

	testDSN = dsn

	return m.Run()
}

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func quietOptions() Options {
	return Options{Logger: quietLogger(), Pool: defaultPoolConfig(), Ping: PingConfig{PingTimeout: 5 * time.Second}}
}

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

func startPostgres() (string, string, error) {
	runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 이미지 pull·platform 경고는 stderr로 나온다. stdout의 container ID만 읽어야 시작한 컨테이너를
	// 정리할 수 있고, 경고가 섞인 ID로 inspect가 실패해 테스트가 조용히 skip되지 않는다.
	out, err := exec.CommandContext(runCtx, "docker", "run", "-d", "--rm",
		"-e", "POSTGRES_PASSWORD=sharedgo-local-test-placeholder",
		"-e", "POSTGRES_DB=sharedgo_test",
		"-P", "postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873",
	).Output()
	if err != nil {
		return "", "", fmt.Errorf("docker run: %w: %s", err, commandStderr(err))
	}

	id := strings.TrimSpace(string(out))

	port, err := mappedPort(id)
	if err != nil {
		removeContainer(id)

		return "", "", fmt.Errorf("mapped port: %w", err)
	}

	dsn := fmt.Sprintf("postgres://postgres:sharedgo-local-test-placeholder@127.0.0.1:%s/sharedgo_test?sslmode=disable", port)
	if err := waitReady(dsn); err != nil {
		removeContainer(id)

		return "", "", fmt.Errorf("wait ready: %w", err)
	}

	return id, dsn, nil
}

func mappedPort(id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "inspect", //nolint:gosec // 테스트가 만든 컨테이너 ID와 고정 명령만 전달한다.
		"--format", `{{ (index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort }}`, id,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w: %s", err, strings.TrimSpace(string(out)))
	}

	port := strings.TrimSpace(string(out))
	if port == "" {
		return "", errors.New("no mapped host port for 5432/tcp")
	}

	return port, nil
}

func waitReady(dsn string) error {
	deadline := time.Now().Add(60 * time.Second)

	var lastErr error

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pool, err := OpenPoolDSN(ctx, dsn, Options{Logger: quietLogger(), Ping: PingConfig{PingTimeout: time.Second}})

		cancel()

		if err == nil {
			pool.Close()

			return nil
		}

		lastErr = err

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("postgres not ready: %w", lastErr)
}

func removeContainer(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := exec.CommandContext(ctx, "docker", "rm", "-f", "-v", id).Run(); err != nil { //nolint:gosec // 테스트가 만든 컨테이너 ID와 고정 명령만 전달한다.
		fmt.Fprintf(os.Stderr, "pgxdb: docker rm -f -v %s failed: %v\n", id, err)
	}
}

func commandStderr(err error) string {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return strings.TrimSpace(string(exitErr.Stderr))
	}

	return ""
}

func requireContainer(t *testing.T) string {
	t.Helper()

	if testDSN == "" {
		t.Skip("docker/postgres unavailable; skipping integration test")
	}

	return testDSN
}

func testConfig(t *testing.T) Config {
	t.Helper()

	u, err := url.Parse(testDSN)
	if err != nil {
		t.Fatalf("parse testDSN: %v", err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	password, _ := u.User.Password()

	return Config{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: password,
		Name:     strings.TrimPrefix(u.Path, "/"),
		SSLMode:  testDisable,
	}
}

func selectOne(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
},
) error {
	var n int

	if err := querier.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		return fmt.Errorf("scan SELECT 1: %w", err)
	}

	if n != 1 {
		return fmt.Errorf("SELECT 1 = %d, want 1", n)
	}

	return nil
}

func TestIntegration_OpenPool(t *testing.T) {
	requireContainer(t)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	pool, err := OpenPool(ctx, testConfig(t), quietOptions())
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	defer pool.Close()

	if err := selectOne(ctx, pool); err != nil {
		t.Fatalf("selectOne() error = %v", err)
	}
}

func TestIntegration_OpenPoolDSN(t *testing.T) {
	dsn := requireContainer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	pool, err := OpenPoolDSN(ctx, dsn, quietOptions())
	if err != nil {
		t.Fatalf("OpenPoolDSN: %v", err)
	}
	defer pool.Close()

	if err := selectOne(ctx, pool); err != nil {
		t.Fatalf("selectOne() error = %v", err)
	}
}

func TestIntegration_AfterConnect(t *testing.T) {
	dsn := requireContainer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	opts := quietOptions()

	opts.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET application_name = 'sharedgo_ac'"); err != nil {
			return fmt.Errorf("set application_name: %w", err)
		}

		return nil
	}

	pool, err := OpenPoolDSN(ctx, dsn, opts)
	if err != nil {
		t.Fatalf("OpenPoolDSN: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	var appName string

	if err := conn.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName); err != nil {
		t.Fatalf("current_setting: %v", err)
	}

	if appName != "sharedgo_ac" {
		t.Fatalf("application_name = %q, want sharedgo_ac (AfterConnect did not run)", appName)
	}
}

func TestIntegration_RollbackCanceledContextReusesConnection(t *testing.T) {
	dsn := requireContainer(t)

	for _, deferred := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("deferred=%t/expired=%t", deferred, expired), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()

				pool, err := OpenPoolDSN(ctx, dsn, Options{Logger: quietLogger(), Pool: PoolConfig{MaxConns: 1}})
				require.NoError(t, err)

				defer pool.Close()

				conn, err := pool.Acquire(ctx)
				require.NoError(t, err)

				defer conn.Release()

				pid := conn.Conn().PgConn().PID()

				tx, err := conn.Begin(ctx)
				require.NoError(t, err)

				_, statementErr := tx.Exec(ctx, "SELECT 1 / 0")
				require.Error(t, statementErr)

				var (
					requestCtx    context.Context
					requestCancel context.CancelFunc
				)

				if expired {
					requestCtx, requestCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				} else {
					requestCtx, requestCancel = context.WithCancel(ctx)
				}

				requestCancel()

				if deferred {
					operationErr := errors.New("request operation failed")

					err = func() (resultErr error) {
						defer RollbackDeferred(requestCtx, tx, &resultErr)

						return operationErr
					}()

					require.ErrorIs(t, err, operationErr)
				} else {
					require.NoError(t, Rollback(requestCtx, tx))
				}

				require.False(t, conn.Conn().IsClosed())
				require.Equal(t, byte('I'), conn.Conn().PgConn().TxStatus())

				conn.Release()

				reused, err := pool.Acquire(ctx)
				require.NoError(t, err)

				defer reused.Release()

				require.Equal(t, pid, reused.Conn().PgConn().PID())
				require.NoError(t, selectOne(ctx, reused))
			})
		}
	}
}

func TestIntegration_OpenPoolDSNPreservesOptionsAfterNormalization(t *testing.T) {
	dsn := requireContainer(t)

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	query := parsed.Query()
	query.Set("sslmode", " DISABLE ")
	query.Set("pool_min_conns", "1")
	query.Set("pool_max_conns", "3")
	query.Set("pool_max_conn_lifetime", "13m")
	query.Set("pool_max_conn_lifetime_jitter", "2m")
	query.Set("pool_max_conn_idle_time", "7m")
	query.Set("pool_health_check_period", "42s")
	query.Set("application_name", "normalized pool")
	query.Set("default_query_exec_mode", "exec")

	parsed.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")

	keywordCfg := testConfig(t)

	keywordDSN := keywordCfg.buildDSN(keywordCfg.Password)

	keywordDSN = strings.Replace(keywordDSN, "sslmode='disable'", "sslmode=' DISABLE '", 1) +
		" pool_min_conns=1 pool_max_conns=3 pool_max_conn_lifetime=13m pool_max_conn_lifetime_jitter=2m pool_max_conn_idle_time=7m pool_health_check_period=42s application_name='normalized pool' default_query_exec_mode=exec"

	multiHostDSN := strings.Replace(parsed.String(), parsed.Host, parsed.Host+",localhost", 1)

	for _, raw := range []string{parsed.String(), keywordDSN, multiHostDSN} {
		for _, maxConns := range []int{0, 4} {
			pool, err := OpenPoolDSN(t.Context(), raw, Options{Logger: quietLogger(), Pool: PoolConfig{MaxConns: maxConns}})
			if err != nil {
				t.Fatal(err)
			}

			cfg := pool.Config()
			pool.Close()

			wantMax := int32(3)

			if maxConns != 0 {
				wantMax = 4
			}

			if cfg.MinConns != 1 || cfg.MaxConns != wantMax || cfg.MaxConnLifetime != 13*time.Minute || cfg.MaxConnLifetimeJitter != 2*time.Minute || cfg.MaxConnIdleTime != 7*time.Minute || cfg.HealthCheckPeriod != 42*time.Second {
				t.Fatal("DSN pool options or explicit overlay changed during normalization")
			}

			if cfg.ConnConfig.RuntimeParams["application_name"] != "normalized pool" || cfg.ConnConfig.DefaultQueryExecMode != pgx.QueryExecModeExec {
				t.Fatal("connection options changed during pool normalization")
			}
		}
	}
}
