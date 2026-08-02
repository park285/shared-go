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
	Ping         PingConfig
	AfterConnect func(ctx context.Context, conn *pgx.Conn) error
}

func DefaultOptions() Options {
	return Options{
		Logger: slog.Default(),
		Pool:   DefaultPoolConfig(),
		Ping:   DefaultPingConfig(),
	}
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

func (o Options) pingTimeout() time.Duration {
	if o.Ping.PingTimeout > 0 {
		return o.Ping.PingTimeout
	}
	return 5 * time.Second
}

func OpenPool(ctx context.Context, cfg Config, opts Options) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()

	return connectConfig(ctx, cfg, opts)
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
