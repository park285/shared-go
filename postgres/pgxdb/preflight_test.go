package pgxdb

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	preflightLoopbackHost = "127.0.0.1"
	preflightDatabaseName = "test"
)

func preflightTarget(t *testing.T) Config {
	t.Helper()

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var dials atomic.Int32

	var workers sync.WaitGroup

	workers.Go(func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			dials.Add(1)
			closeTestSocket(t, conn)
		}
	})
	t.Cleanup(func() {
		closeTestSocket(t, listener)
		workers.Wait()
		require.Zero(t, dials.Load(), "로컬 준비 단계에서 접속을 시도했다")
	})

	address, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	return Config{Host: preflightLoopbackHost, Port: address.Port, User: preflightDatabaseName, Password: "preflight-secret-sentinel", Name: preflightDatabaseName, SSLMode: testDisable}
}

func TestPreflightRejectsBeforeDial(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config, *Options)
	}{
		{"missing sslmode", func(cfg *Config, _ *Options) { cfg.SSLMode = "" }},
		{"invalid sslmode", func(cfg *Config, _ *Options) { cfg.SSLMode = "invalid" }},
		{"invalid query mode", func(cfg *Config, _ *Options) { cfg.QueryExecMode = "invalid" }},
		{"invalid port", func(cfg *Config, _ *Options) { cfg.Port = -1 }},
		{"missing startup timeout", func(_ *Config, opts *Options) { opts.Ping.PingTimeout = 0 }},
		{"missing maximum", func(_ *Config, opts *Options) { opts.Pool.MaxConns = 0 }},
		{"missing lifetime", func(_ *Config, opts *Options) { opts.Pool.ConnMaxLifetime = 0 }},
		{"missing idle lifetime", func(_ *Config, opts *Options) { opts.Pool.ConnMaxIdleTime = 0 }},
		{"negative jitter", func(_ *Config, opts *Options) { opts.Pool.ConnMaxLifetimeJitter = -1 }},
		{"inverted counts", func(_ *Config, opts *Options) { opts.Pool.MinConns = opts.Pool.MaxConns + 1 }},
		{"exhausted reserve", func(_ *Config, opts *Options) {
			opts.Pool.MaxConns = 1
			opts.AcquirePolicy = &AcquirePolicy{ReservedConns: 1, Timeout: time.Second}
		}},
		{"negative reserve", func(_ *Config, opts *Options) {
			opts.AcquirePolicy = &AcquirePolicy{ReservedConns: -1, Timeout: time.Second}
		}},
		{"missing acquire timeout", func(_ *Config, opts *Options) { opts.AcquirePolicy = &AcquirePolicy{} }},
		{"cancel deadline", func(_ *Config, opts *Options) {
			opts.CancelRequest = &CancelRequestConfig{CancelRequestDelay: time.Second, DeadlineDelay: time.Second}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := preflightTarget(t)
			opts := quietOptions()
			tc.change(&cfg, &opts)

			preflightErr := Preflight(cfg, opts)
			requireConfigurationFailure(t, preflightErr)

			pool, openErr := OpenPool(t.Context(), cfg, opts)
			require.Nil(t, pool)
			requireConfigurationFailure(t, openErr)
			require.EqualError(t, openErr, preflightErr.Error())
			require.NotContains(t, openErr.Error(), cfg.Password)
		})
	}
}

func TestPreflightDSNEffectiveCapacityBeforeDial(t *testing.T) {
	for _, tc := range []struct {
		name    string
		params  string
		max     int
		reserve int32
		valid   bool
	}{
		{name: "zero overlay preserves capacity", params: "pool_max_conns=3", reserve: 2, valid: true},
		{name: "zero overlay exhausts reserve", params: "pool_max_conns=2", reserve: 2},
		{name: "overlay increases capacity", params: "pool_max_conns=2", max: 3, reserve: 2, valid: true},
		{name: "overlay reduces capacity", params: "pool_max_conns=3", max: 2, reserve: 2},
		{name: "minimum exceeds overlay", params: "pool_max_conns=4 pool_min_conns=3", max: 2},
		{name: "idle minimum exceeds overlay", params: "pool_max_conns=4 pool_min_idle_conns=3", max: 2},
		{name: "invalid parsed maximum", params: "pool_max_conns=0", max: 3},
		{name: "invalid parsed health period", params: "pool_health_check_period=0s", max: 3},
		{name: "invalid parsed lifetime", params: "pool_max_conn_lifetime=-1s", max: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := preflightTarget(t)
			dsn := cfg.buildDSN(cfg.Password) + " " + tc.params
			opts := Options{Logger: quietLogger(), Ping: testPing(), Pool: PoolConfig{MaxConns: tc.max}, AcquirePolicy: &AcquirePolicy{ReservedConns: tc.reserve, Timeout: time.Second}}
			preflightErr := PreflightDSN(dsn, opts)

			if tc.valid {
				require.NoError(t, preflightErr)

				return
			}

			requireConfigurationFailure(t, preflightErr)

			pool, openErr := OpenPoolDSN(t.Context(), dsn, opts)
			require.Nil(t, pool)
			requireConfigurationFailure(t, openErr)
			require.EqualError(t, openErr, preflightErr.Error())
			require.NotContains(t, openErr.Error(), cfg.Password)
			require.NotContains(t, openErr.Error(), dsn)
		})
	}
}

func TestPreflightParsesLocalCAWithoutDial(t *testing.T) {
	cfg := preflightTarget(t)

	cfg.SSLMode = testVerifyFull
	cfg.SSLRootCert = filepath.Join(t.TempDir(), "root.pem")

	opts := quietOptions()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfg.SSLRootCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, Preflight(cfg, opts))
	require.NoError(t, PreflightDSN(cfg.buildDSN(cfg.Password), opts))

	for _, content := range []string{"", "invalid CA preflight-secret-sentinel"} {
		require.NoError(t, os.WriteFile(cfg.SSLRootCert, []byte(content), 0o600))
		assertPreflightCAFailure(t, cfg, opts)
	}

	require.NoError(t, os.Remove(cfg.SSLRootCert))
	assertPreflightCAFailure(t, cfg, opts)
}

func assertPreflightCAFailure(t *testing.T, cfg Config, opts Options) {
	t.Helper()

	preflightErr := Preflight(cfg, opts)
	requireConfigurationFailure(t, preflightErr)

	pool, openErr := OpenPool(t.Context(), cfg, opts)
	require.Nil(t, pool)
	require.EqualError(t, openErr, preflightErr.Error())
	require.NotContains(t, openErr.Error(), cfg.Password)

	dsn := cfg.buildDSN(cfg.Password)

	preflightErr = PreflightDSN(dsn, opts)
	requireConfigurationFailure(t, preflightErr)

	pool, openErr = OpenPoolDSN(t.Context(), dsn, opts)
	require.Nil(t, pool)
	require.EqualError(t, openErr, preflightErr.Error())
	require.NotContains(t, openErr.Error(), cfg.Password)
	require.NotContains(t, openErr.Error(), dsn)
}

func TestPreflightSuccessDoesNotHideStartupFailure(t *testing.T) {
	cfg := Config{Host: preflightLoopbackHost, Port: 1, User: preflightDatabaseName, Name: preflightDatabaseName, SSLMode: testDisable}
	opts := quietOptions()
	require.NoError(t, Preflight(cfg, opts))
	require.NoError(t, PreflightDSN(cfg.buildDSN(""), opts))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	pool, err := OpenPool(ctx, cfg, opts)
	require.Nil(t, pool)
	require.ErrorIs(t, err, context.Canceled)

	pool, err = OpenPoolDSN(ctx, cfg.buildDSN(""), opts)
	require.Nil(t, pool)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIntegrationPreflightOpenOptionsParity(t *testing.T) {
	dsn := requireContainer(t)
	cfg := testConfig(t)

	for _, structured := range []bool{true, false} {
		t.Run(map[bool]string{true: "structured", false: "dsn"}[structured], func(t *testing.T) {
			var connected atomic.Int32

			opts := Options{
				Logger: quietLogger(), Pool: PoolConfig{MaxConns: 3, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: time.Minute, AcquirePingTimeout: time.Second}, Ping: testPing(),
				RuntimeParams: map[string]string{"application_name": "preflight-options", "statement_timeout": "3s"},
				CancelRequest: &CancelRequestConfig{CancelRequestDelay: time.Millisecond, DeadlineDelay: time.Second},
				AcquirePolicy: &AcquirePolicy{ReservedConns: 1, Timeout: 100 * time.Millisecond},
				AfterConnect: func(context.Context, *pgx.Conn) error {
					connected.Add(1)

					return nil
				},
			}

			var pool *pgxpool.Pool

			var err error

			if structured {
				require.NoError(t, Preflight(cfg, opts))
				require.Zero(t, connected.Load())

				pool, err = OpenPool(t.Context(), cfg, opts)
			} else {
				require.NoError(t, PreflightDSN(dsn+"&pool_max_conns=8", opts))
				require.Zero(t, connected.Load())

				pool, err = OpenPoolDSN(t.Context(), dsn+"&pool_max_conns=8", opts)
			}

			require.NoError(t, err)
			t.Cleanup(pool.Close)
			require.Positive(t, connected.Load())
			require.EqualValues(t, 3, pool.Config().MaxConns)
			require.Equal(t, time.Second, pool.Config().PingTimeout)

			watcher := pool.Config().ConnConfig.BuildContextWatcherHandler(&pgconn.PgConn{})
			require.IsType(t, &pgconn.CancelRequestContextWatcherHandler{}, watcher)

			var application, timeout string

			require.NoError(t, pool.QueryRow(t.Context(), "SELECT current_setting('application_name'), current_setting('statement_timeout')").Scan(&application, &timeout))
			require.Equal(t, "preflight-options", application)
			require.Equal(t, "3s", timeout)

			first, err := pool.Acquire(t.Context())
			require.NoError(t, err)

			defer first.Release()

			second, err := pool.Acquire(t.Context())
			require.NoError(t, err)

			defer second.Release()

			ordinary, err := pool.Acquire(t.Context())
			require.Nil(t, ordinary)
			require.ErrorIs(t, err, context.DeadlineExceeded)

			control, err := pool.Acquire(WithControlAcquire(t.Context()))
			require.NoError(t, err)
			control.Release()
		})
	}
}
