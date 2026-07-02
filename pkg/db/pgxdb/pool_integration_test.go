package pgxdb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func quietOptions() Options {
	return Options{Logger: quietLogger(), Pool: DefaultPoolConfig(), Retry: DefaultRetryConfig()}
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
	out, err := exec.CommandContext(runCtx, "docker", "run", "-d", "--rm",
		"-e", "POSTGRES_PASSWORD=sharedgo_test",
		"-e", "POSTGRES_DB=sharedgo_test",
		"-P", "postgres:16-alpine",
	).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(string(out)))
	}
	id := strings.TrimSpace(string(out))
	port, err := mappedPort(id)
	if err != nil {
		removeContainer(id)
		return "", "", err
	}
	dsn := fmt.Sprintf("postgres://postgres:sharedgo_test@127.0.0.1:%s/sharedgo_test?sslmode=disable", port)
	if err := waitReady(dsn); err != nil {
		removeContainer(id)
		return "", "", err
	}
	return id, dsn, nil
}

func mappedPort(id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "inspect",
		"--format", `{{ (index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort }}`, id,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w: %s", err, strings.TrimSpace(string(out)))
	}
	port := strings.TrimSpace(string(out))
	if port == "" {
		return "", fmt.Errorf("no mapped host port for 5432/tcp")
	}
	return port, nil
}

func waitReady(dsn string) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pool, err := OpenPoolDSN(ctx, dsn, Options{Logger: quietLogger(), Retry: RetryConfig{PingTimeout: time.Second}})
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
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", id).Run()
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
		SSLMode:  "disable",
	}
}

func selectOne(t *testing.T, ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) {
	t.Helper()
	var n int
	if err := querier.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if n != 1 {
		t.Fatalf("SELECT 1 = %d, want 1", n)
	}
}

func TestIntegration_OpenPool(t *testing.T) {
	requireContainer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := OpenPool(ctx, testConfig(t), quietOptions())
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	defer pool.Close()
	selectOne(t, ctx, pool)
}

func TestIntegration_OpenPoolDSN(t *testing.T) {
	dsn := requireContainer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := OpenPoolDSN(ctx, dsn, quietOptions())
	if err != nil {
		t.Fatalf("OpenPoolDSN: %v", err)
	}
	defer pool.Close()
	selectOne(t, ctx, pool)
}

func TestIntegration_OpenPoolWithRetry(t *testing.T) {
	requireContainer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := OpenPoolWithRetry(ctx, testConfig(t), quietOptions())
	if err != nil {
		t.Fatalf("OpenPoolWithRetry: %v", err)
	}
	defer pool.Close()
	selectOne(t, ctx, pool)
}

func TestIntegration_AfterConnect(t *testing.T) {
	dsn := requireContainer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	opts := quietOptions()
	opts.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET application_name = 'sharedgo_ac'")
		return err
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
