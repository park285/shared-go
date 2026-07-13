package pgxdb

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/pkg/retry"
)

var queryExecModes = map[string]pgx.QueryExecMode{
	"cache_statement": pgx.QueryExecModeCacheStatement,
	"cache_describe":  pgx.QueryExecModeCacheDescribe,
	"describe_exec":   pgx.QueryExecModeDescribeExec,
	"exec":            pgx.QueryExecModeExec,
	"simple_protocol": pgx.QueryExecModeSimpleProtocol,
}

type Options struct {
	Logger       *slog.Logger
	Pool         PoolConfig
	Retry        RetryConfig
	DNSFallback  bool
	AfterConnect func(ctx context.Context, conn *pgx.Conn) error
}

func DefaultOptions() Options {
	return Options{
		Logger: slog.Default(),
		Pool:   DefaultPoolConfig(),
		Retry:  DefaultRetryConfig(),
	}
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

func (o Options) pingTimeout() time.Duration {
	if o.Retry.PingTimeout > 0 {
		return o.Retry.PingTimeout
	}
	return 5 * time.Second
}

func OpenPool(ctx context.Context, cfg Config, opts Options) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()

	pool, err := connectConfig(ctx, cfg, opts)
	if err == nil {
		return pool, nil
	}
	if !opts.DNSFallback || !ShouldFallbackToLocalhost(err, cfg.Host) {
		return nil, err
	}

	fallback := cfg
	fallback.Host = "127.0.0.1"
	pool, fallbackErr := connectConfig(ctx, fallback, opts)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	opts.Logger.Warn("postgres_host_fallback",
		slog.String("configured_host", cfg.Host),
		slog.String("effective_host", fallback.Host),
	)
	return pool, nil
}

func OpenPoolDSN(ctx context.Context, rawDSN string, opts Options) (*pgxpool.Pool, error) {
	normalizedDSN := strings.TrimSpace(rawDSN)
	if normalizedDSN == "" {
		return nil, fmt.Errorf("pgxdb: dsn is required")
	}
	if err := validateExplicitSSLMode(normalizedDSN); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()

	poolCfg, err := pgxpool.ParseConfig(normalizedDSN)
	if err != nil {
		return nil, fmt.Errorf("pgxdb: parse dsn: %w", err)
	}
	if err := overlayPoolConfig(poolCfg, opts.Pool); err != nil {
		return nil, err
	}
	poolCfg.AfterConnect = opts.AfterConnect
	return newPoolAndPing(ctx, poolCfg, opts)
}

func OpenPoolWithRetry(ctx context.Context, cfg Config, opts Options) (*pgxpool.Pool, error) {
	opts = opts.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := validateConnCounts(withPoolDefaults(opts.Pool)); err != nil {
		return nil, err
	}
	r := normalizeRetry(opts.Retry)

	var pool *pgxpool.Pool
	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: r.MaxAttempts,
		BaseDelay:   r.BaseDelay,
		MaxDelay:    r.MaxDelay,
		ShouldRetry: func(err error) bool {
			return isRetryableConnectError(ctx, err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			opts.Logger.Warn("postgres_connect_retry",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		},
	}, func(ctx context.Context) error {
		p, openErr := OpenPool(ctx, cfg, opts)
		if openErr != nil {
			return openErr
		}
		pool = p
		return nil
	})
	if err != nil {
		if isRetryableConnectError(ctx, err) {
			return nil, fmt.Errorf("pgxdb: open pool after retries: %w", err)
		}
		return nil, fmt.Errorf("pgxdb: open pool: %w", err)
	}
	return pool, nil
}

func connectConfig(ctx context.Context, cfg Config, opts Options) (*pgxpool.Pool, error) {
	poolCfg, err := buildConfigPool(&cfg, opts)
	if err != nil {
		return nil, err
	}
	return newPoolAndPing(ctx, poolCfg, opts)
}

func buildConfigPool(cfg *Config, opts Options) (*pgxpool.Config, error) {
	safeDSN, err := cfg.SafeDSN()
	if err != nil {
		return nil, err
	}
	poolCfg, err := pgxpool.ParseConfig(safeDSN)
	if err != nil {
		return nil, fmt.Errorf("pgxdb: parse config: %w", err)
	}
	poolCfg.ConnConfig.Password = cfg.Password
	if err := applyQueryExecMode(poolCfg, cfg.QueryExecMode); err != nil {
		return nil, err
	}
	if err := applyPoolConfig(poolCfg, withPoolDefaults(opts.Pool)); err != nil {
		return nil, err
	}
	poolCfg.AfterConnect = opts.AfterConnect
	return poolCfg, nil
}

func newPoolAndPing(ctx context.Context, poolCfg *pgxpool.Config, opts Options) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("pgxdb: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, opts.pingTimeout())
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgxdb: ping: %w", err)
	}

	mode := "tcp"
	if poolCfg.ConnConfig.Host != "" && strings.HasPrefix(poolCfg.ConnConfig.Host, "/") {
		mode = "uds"
	}
	opts.Logger.Info("postgres_pool_connected",
		slog.String("mode", mode),
		slog.String("host", poolCfg.ConnConfig.Host),
		slog.Int("port", int(poolCfg.ConnConfig.Port)),
		slog.String("database", poolCfg.ConnConfig.Database),
		slog.Int("min_conns", int(poolCfg.MinConns)),
		slog.Int("max_conns", int(poolCfg.MaxConns)),
	)
	return pool, nil
}

func applyQueryExecMode(poolCfg *pgxpool.Config, queryExecMode string) error {
	if queryExecMode == "" {
		return nil
	}
	mode, ok := queryExecModes[normalizeQueryExecMode(queryExecMode)]
	if !ok {
		return fmt.Errorf("pgxdb: invalid query exec mode %q (allowed: cache_statement, cache_describe, describe_exec, exec, simple_protocol)", queryExecMode)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = mode
	return nil
}

func applyPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	if err := validateConnCounts(pool); err != nil {
		return err
	}
	poolCfg.MinConns = int32(pool.MinConns)
	poolCfg.MaxConns = int32(pool.MaxConns)
	poolCfg.MaxConnLifetime = pool.ConnMaxLifetime
	poolCfg.MaxConnLifetimeJitter = pool.ConnMaxLifetimeJitter
	poolCfg.MaxConnIdleTime = pool.ConnMaxIdleTime
	return nil
}

func overlayPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	if err := validateConnCounts(pool); err != nil {
		return err
	}
	if pool.MinConns > 0 {
		poolCfg.MinConns = int32(pool.MinConns)
	}
	if pool.MaxConns > 0 {
		poolCfg.MaxConns = int32(pool.MaxConns)
	}
	overlayLifetime(poolCfg, pool)
	if pool.ConnMaxIdleTime > 0 {
		poolCfg.MaxConnIdleTime = pool.ConnMaxIdleTime
	}
	return nil
}

func overlayLifetime(poolCfg *pgxpool.Config, pool PoolConfig) {
	if pool.ConnMaxLifetime > 0 {
		poolCfg.MaxConnLifetime = pool.ConnMaxLifetime
		if pool.ConnMaxLifetimeJitter > 0 {
			poolCfg.MaxConnLifetimeJitter = pool.ConnMaxLifetimeJitter
		} else {
			poolCfg.MaxConnLifetimeJitter = pool.ConnMaxLifetime / 5
		}
		return
	}
	if pool.ConnMaxLifetimeJitter > 0 {
		poolCfg.MaxConnLifetimeJitter = pool.ConnMaxLifetimeJitter
	}
}

func validateConnCounts(pool PoolConfig) error {
	if pool.MinConns < 0 || pool.MaxConns < 0 || pool.MinConns > math.MaxInt32 || pool.MaxConns > math.MaxInt32 {
		return fmt.Errorf("pgxdb: pool connection count out of int32 range: min=%d max=%d", pool.MinConns, pool.MaxConns)
	}
	return nil
}

func withPoolDefaults(pool PoolConfig) PoolConfig {
	def := DefaultPoolConfig()
	if pool.MinConns <= 0 {
		pool.MinConns = def.MinConns
	}
	if pool.MaxConns <= 0 {
		pool.MaxConns = def.MaxConns
	}
	if pool.ConnMaxLifetime <= 0 {
		pool.ConnMaxLifetime = def.ConnMaxLifetime
	}
	if pool.ConnMaxLifetimeJitter <= 0 {
		pool.ConnMaxLifetimeJitter = pool.ConnMaxLifetime / 5
	}
	if pool.ConnMaxIdleTime <= 0 {
		pool.ConnMaxIdleTime = def.ConnMaxIdleTime
	}
	return pool
}

func normalizeRetry(r RetryConfig) RetryConfig {
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = 5
	}
	if r.BaseDelay <= 0 {
		r.BaseDelay = 2 * time.Second
	}
	if r.MaxDelay <= 0 {
		r.MaxDelay = 30 * time.Second
	}
	return r
}
