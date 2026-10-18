package pgxdb

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func requireConfigurationFailure(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)

	_, connected := errors.AsType[*pgconn.ConnectError](err)
	require.False(t, connected, "invalid configuration reached a network connection attempt")
}

func TestPoolOptionsRejectInvalidValuesBeforeConnect(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 1, User: "test", Name: "test", SSLMode: testDisable}
	cases := []struct {
		name string
		opts Options
	}{
		{"negative minimum", Options{Pool: PoolConfig{MaxConns: 1, MinConns: -1}}},
		{"negative maximum", Options{Pool: PoolConfig{MaxConns: -1}}},
		{"negative lifetime", Options{Pool: PoolConfig{MaxConns: 1, ConnMaxLifetime: -1}}},
		{"negative jitter", Options{Pool: PoolConfig{MaxConns: 1, ConnMaxLifetimeJitter: -1}}},
		{"negative idle", Options{Pool: PoolConfig{MaxConns: 1, ConnMaxIdleTime: -1}}},
		{"negative health period", Options{Pool: PoolConfig{MaxConns: 1, HealthCheckPeriod: -1}}},
		{"negative acquire ping", Options{Pool: PoolConfig{MaxConns: 1, PingTimeout: -1}}},
		{"negative startup ping", Options{Pool: PoolConfig{MaxConns: 1}, Ping: PingConfig{PingTimeout: -1}}},
		{"negative cancel delay", Options{Pool: PoolConfig{MaxConns: 1}, CancelRequest: &CancelRequestConfig{CancelRequestDelay: -1, DeadlineDelay: time.Second}}},
		{"zero cancel deadline", Options{Pool: PoolConfig{MaxConns: 1}, CancelRequest: &CancelRequestConfig{}}},
		{"equal cancel delays", Options{Pool: PoolConfig{MaxConns: 1}, CancelRequest: &CancelRequestConfig{CancelRequestDelay: time.Second, DeadlineDelay: time.Second}}},
		{"inverted cancel delays", Options{Pool: PoolConfig{MaxConns: 1}, CancelRequest: &CancelRequestConfig{CancelRequestDelay: time.Second, DeadlineDelay: time.Millisecond}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenPool(t.Context(), cfg, tc.opts)
			requireConfigurationFailure(t, err)

			_, err = OpenPoolDSN(t.Context(), "postgres://test@127.0.0.1:1/test?sslmode=disable", tc.opts)
			requireConfigurationFailure(t, err)
		})
	}

	_, err := OpenPool(t.Context(), cfg, Options{})
	requireConfigurationFailure(t, err)

	parsed, err := buildConfigPool(&cfg, Options{Pool: PoolConfig{MaxConns: 2}})
	require.NoError(t, err)
	require.Zero(t, parsed.MinConns)
	require.EqualValues(t, 2, parsed.MaxConns)
}

func TestPoolDSNRejectsExplicitInvalidValuesBeforeOverlay(t *testing.T) {
	for _, param := range []string{
		"pool_max_conns=0", "pool_max_conns=-1", "pool_min_conns=-1", "pool_min_idle_conns=-1",
		"pool_max_conn_lifetime=-1s", "pool_max_conn_lifetime_jitter=-1s", "pool_max_conn_idle_time=-1s",
		"pool_health_check_period=0s", "pool_health_check_period=-1s", "pool_ping_timeout=-1s",
	} {
		t.Run(param, func(t *testing.T) {
			_, err := OpenPoolDSN(t.Context(), "postgres://test@127.0.0.1:1/test?sslmode=disable&"+param, DefaultOptions())
			requireConfigurationFailure(t, err)
		})
	}
}

func TestConnectionOptionsPreserveParametersAndSnapshotCancellation(t *testing.T) {
	cfg := mustParse(t, "postgres://test@127.0.0.1:1/test?sslmode=disable&application_name=dsn&search_path=public")
	params := map[string]string{"application_name": "runtime", "statement_timeout": "10s"}
	policy := &CancelRequestConfig{DeadlineDelay: time.Second}
	applyConnectionOptions(cfg, Options{RuntimeParams: params, CancelRequest: policy})
	require.Equal(t, "runtime", cfg.ConnConfig.RuntimeParams["application_name"])
	require.Equal(t, "public", cfg.ConnConfig.RuntimeParams["search_path"])
	require.Len(t, params, 2)

	params["statement_timeout"] = "1ms"
	policy.DeadlineDelay = time.Hour

	require.Equal(t, "10s", cfg.ConnConfig.RuntimeParams["statement_timeout"])

	cfg.ConnConfig.RuntimeParams["application_name"] = "connection"

	require.Equal(t, "runtime", params["application_name"])

	conn := &pgconn.PgConn{}
	first, ok := cfg.ConnConfig.BuildContextWatcherHandler(conn).(*pgconn.CancelRequestContextWatcherHandler)
	require.True(t, ok)

	second, ok := cfg.ConnConfig.BuildContextWatcherHandler(conn).(*pgconn.CancelRequestContextWatcherHandler)
	require.True(t, ok)
	require.NotSame(t, first, second)
	require.Same(t, conn, first.Conn)
	require.Equal(t, time.Second, first.DeadlineDelay)
	require.Zero(t, first.CancelRequestDelay)

	defaultCfg := mustParse(t, "postgres://test@127.0.0.1:1/test?sslmode=disable")
	applyConnectionOptions(defaultCfg, Options{})
	require.IsType(t, &pgconn.DeadlineContextWatcherHandler{}, defaultCfg.ConnConfig.BuildContextWatcherHandler(conn))
	require.NotContains(t, defaultCfg.ConnConfig.RuntimeParams, "statement_timeout")
}

func TestAcquirePingAndStartupPingRemainIndependent(t *testing.T) {
	cfg := mustParse(t, "postgres://test@127.0.0.1:1/test?sslmode=disable&pool_ping_timeout=2s")
	require.NoError(t, overlayPoolConfig(cfg, PoolConfig{}))
	require.Equal(t, 2*time.Second, cfg.PingTimeout)

	opts := Options{Pool: PoolConfig{MaxConns: 2, PingTimeout: 250 * time.Millisecond}, Ping: PingConfig{PingTimeout: 3 * time.Second}}
	require.NoError(t, overlayPoolConfig(cfg, opts.Pool))
	require.Equal(t, 250*time.Millisecond, cfg.PingTimeout)
	require.Equal(t, 3*time.Second, opts.pingTimeout())

	structured := Config{Host: "127.0.0.1", Port: 1, SSLMode: testDisable}
	built, err := buildConfigPool(&structured, opts)
	require.NoError(t, err)
	require.Equal(t, 250*time.Millisecond, built.PingTimeout)
}
