package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
		return nil, fmt.Errorf("validate: %w", err)
	}

	opts = opts.withDefaults()

	out, err := connectConfig(ctx, cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("connect config: %w", err)
	}

	return out, nil
}

func OpenPoolDSN(ctx context.Context, rawDSN string, opts Options) (*pgxpool.Pool, error) {
	normalizedDSN := strings.TrimSpace(rawDSN)
	if normalizedDSN == "" {
		return nil, errors.New("pgxdb: dsn is required")
	}

	if err := validateExplicitSSLMode(normalizedDSN); err != nil {
		return nil, fmt.Errorf("validate explicit SSL mode: %w", err)
	}

	opts = opts.withDefaults()

	poolCfg, err := pgxpool.ParseConfig(normalizedDSN)
	if err != nil {
		return nil, fmt.Errorf("pgxdb: parse dsn: %w", err)
	}

	if overlayErr := overlayPoolConfig(poolCfg, opts.Pool); overlayErr != nil {
		return nil, fmt.Errorf("overlay pool config: %w", overlayErr)
	}

	poolCfg.AfterConnect = opts.AfterConnect

	out, err := newPoolAndPing(ctx, poolCfg, opts)
	if err != nil {
		return nil, fmt.Errorf("pool and ping: %w", err)
	}

	return out, nil
}

func connectConfig(ctx context.Context, cfg Config, opts Options) (*pgxpool.Pool, error) {
	poolCfg, err := buildConfigPool(&cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("build config pool: %w", err)
	}

	out, err := newPoolAndPing(ctx, poolCfg, opts)
	if err != nil {
		return nil, fmt.Errorf("pool and ping: %w", err)
	}

	return out, nil
}

func buildConfigPool(cfg *Config, opts Options) (*pgxpool.Config, error) {
	safeDSN, err := cfg.SafeDSN()
	if err != nil {
		return nil, fmt.Errorf("safe DSN: %w", err)
	}

	poolCfg, err := pgxpool.ParseConfig(safeDSN)
	if err != nil {
		return nil, fmt.Errorf("pgxdb: parse config: %w", err)
	}

	poolCfg.ConnConfig.Password = cfg.Password
	if err := applyPoolConfig(poolCfg, withPoolDefaults(opts.Pool)); err != nil {
		return nil, fmt.Errorf("apply pool config: %w", err)
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

func applyPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	minConns, maxConns, err := connCountsInt32(pool)
	if err != nil {
		return fmt.Errorf("validate conn counts: %w", err)
	}

	poolCfg.MinConns = minConns
	poolCfg.MaxConns = maxConns
	poolCfg.MaxConnLifetime = pool.ConnMaxLifetime
	poolCfg.MaxConnLifetimeJitter = pool.ConnMaxLifetimeJitter
	poolCfg.MaxConnIdleTime = pool.ConnMaxIdleTime
	applyHealthCheckPeriod(poolCfg, pool)

	return nil
}

// 0 대입은 pgxpool의 time.NewTicker(0)를 panic시키므로, 미설정이면 ParseConfig가 채운 값을 남긴다.
func applyHealthCheckPeriod(poolCfg *pgxpool.Config, pool PoolConfig) {
	if pool.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = pool.HealthCheckPeriod
	}
}

func overlayPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	overlayMin, overlayMax, err := connCountsInt32(pool)
	if err != nil {
		return fmt.Errorf("validate conn counts: %w", err)
	}

	minConns, maxConns := poolCfg.MinConns, poolCfg.MaxConns
	if pool.MinConns > 0 {
		minConns = overlayMin
	}

	if pool.MaxConns > 0 {
		maxConns = overlayMax
	}

	// pgx는 MinConns > MaxConns를 거부하지 않고 health check 틱마다 초과분 생성을 재시도한다.
	if minConns > maxConns {
		return fmt.Errorf("pgxdb: pool min conns %d exceeds max conns %d", minConns, maxConns)
	}

	poolCfg.MinConns = minConns
	poolCfg.MaxConns = maxConns
	overlayLifetime(poolCfg, pool)

	if pool.ConnMaxIdleTime > 0 {
		poolCfg.MaxConnIdleTime = pool.ConnMaxIdleTime
	}

	applyHealthCheckPeriod(poolCfg, pool)

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
	if _, _, err := connCountsInt32(pool); err != nil {
		return fmt.Errorf("conn counts int32: %w", err)
	}

	return nil
}

func connCountsInt32(pool PoolConfig) (int32, int32, error) {
	minConns, maxConns := pool.MinConns, pool.MaxConns
	if minConns < 0 || maxConns < 0 || minConns > math.MaxInt32 || maxConns > math.MaxInt32 {
		return 0, 0, fmt.Errorf("pgxdb: pool connection count out of int32 range: min=%d max=%d", minConns, maxConns)
	}

	// overlay 경로에서 0은 "미설정"이므로 역전 검사는 둘 다 명시된 경우에만 성립한다.
	if minConns > 0 && maxConns > 0 && minConns > maxConns {
		return 0, 0, fmt.Errorf("pgxdb: pool min conns %d exceeds max conns %d", minConns, maxConns)
	}

	return int32(minConns), int32(maxConns), nil
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
