package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	Logger       *slog.Logger
	Pool         PoolConfig
	Ping         PingConfig
	AfterConnect func(ctx context.Context, conn *pgx.Conn) error
	// RuntimeParams overrides session startup parameters without retaining the map.
	RuntimeParams map[string]string
	// Nil preserves pgx's default cancellation handler.
	CancelRequest *CancelRequestConfig
	// Nil preserves native pgxpool acquisition; non-nil is copied at construction.
	AcquirePolicy *AcquirePolicy
}

// CancelRequestConfig opts into server cancellation with a bounded socket deadline.
// Receiving a cancellation response does not establish the outcome of a lost COMMIT.
type CancelRequestConfig struct {
	CancelRequestDelay time.Duration
	DeadlineDelay      time.Duration
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
	if err := opts.validate(); err != nil {
		return nil, err
	}

	normalizedDSN, err := normalizeDSN(rawDSN)
	if err != nil {
		return nil, err
	}

	opts = opts.withDefaults()

	poolCfg, err := pgxpool.ParseConfig(normalizedDSN)
	if err != nil {
		return nil, errors.New("pgxdb: parse dsn: invalid pool configuration")
	}

	if validationErr := validateParsedPoolConfig(poolCfg); validationErr != nil {
		return nil, validationErr
	}

	if overlayErr := overlayPoolConfig(poolCfg, opts.Pool); overlayErr != nil {
		return nil, fmt.Errorf("overlay pool config: %w", overlayErr)
	}

	applyConnectionOptions(poolCfg, opts)

	if policyErr := installAcquirePolicy(poolCfg, opts.AcquirePolicy); policyErr != nil {
		return nil, policyErr
	}

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
	if err := opts.validate(); err != nil {
		return nil, err
	}

	if opts.Pool.MaxConns <= 0 {
		return nil, errors.New("pgxdb: structured pool requires positive max conns")
	}

	safeDSN, err := cfg.SafeDSN()
	if err != nil {
		return nil, fmt.Errorf("safe DSN: %w", err)
	}

	poolCfg, err := pgxpool.ParseConfig(safeDSN)
	if err != nil {
		return nil, errors.New("pgxdb: parse config: invalid pool configuration")
	}

	poolCfg.ConnConfig.Password = cfg.Password
	if err := applyPoolConfig(poolCfg, withPoolDefaults(opts.Pool)); err != nil {
		return nil, fmt.Errorf("apply pool config: %w", err)
	}

	applyConnectionOptions(poolCfg, opts)

	if err := installAcquirePolicy(poolCfg, opts.AcquirePolicy); err != nil {
		return nil, err
	}

	return poolCfg, nil
}

func newPoolAndPing(ctx context.Context, poolCfg *pgxpool.Config, opts Options) (*pgxpool.Pool, error) {
	boundInitialConnectTimeout(poolCfg, opts.pingTimeout())

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

// The clamp persists for the entire pool lifetime, including reconnects. Pgx's
// minimum-pool background constructors can outlive the startup Ping context.
func boundInitialConnectTimeout(poolCfg *pgxpool.Config, pingTimeout time.Duration) {
	if poolCfg == nil || poolCfg.ConnConfig == nil || poolCfg.MinConns <= 0 || pingTimeout <= 0 {
		return
	}

	if current := poolCfg.ConnConfig.ConnectTimeout; current <= 0 || current > pingTimeout {
		poolCfg.ConnConfig.ConnectTimeout = pingTimeout
	}
}

func applyPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	if err := validatePoolDurations(pool); err != nil {
		return err
	}

	if pool.MaxConns <= 0 {
		return errors.New("pgxdb: structured pool requires positive max conns")
	}

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

	if pool.PingTimeout > 0 {
		poolCfg.PingTimeout = pool.PingTimeout
	}
}

func overlayPoolConfig(poolCfg *pgxpool.Config, pool PoolConfig) error {
	if err := validatePoolDurations(pool); err != nil {
		return err
	}

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

	if pool.ConnMaxLifetime == 0 {
		pool.ConnMaxLifetime = def.ConnMaxLifetime
	}

	if pool.ConnMaxLifetimeJitter == 0 {
		pool.ConnMaxLifetimeJitter = pool.ConnMaxLifetime / 5
	}

	if pool.ConnMaxIdleTime == 0 {
		pool.ConnMaxIdleTime = def.ConnMaxIdleTime
	}

	return pool
}

func (o Options) validate() error {
	if err := validateConnCounts(o.Pool); err != nil {
		return err
	}

	if err := validatePoolDurations(o.Pool); err != nil {
		return err
	}

	if o.Ping.PingTimeout < 0 {
		return errors.New("pgxdb: startup ping timeout must not be negative")
	}

	if c := o.CancelRequest; c != nil && (c.CancelRequestDelay < 0 || c.DeadlineDelay <= 0 || c.DeadlineDelay <= c.CancelRequestDelay) {
		return errors.New("pgxdb: cancel request requires nonnegative delay and a larger positive deadline delay")
	}

	if p := o.AcquirePolicy; p != nil && (p.ReservedConns < 0 || p.Timeout <= 0) {
		return errors.New("pgxdb: acquire policy requires nonnegative reserve and positive timeout")
	}

	return nil
}

func validatePoolDurations(pool PoolConfig) error {
	if pool.ConnMaxLifetime < 0 || pool.ConnMaxLifetimeJitter < 0 || pool.ConnMaxIdleTime < 0 || pool.HealthCheckPeriod < 0 || pool.PingTimeout < 0 {
		return errors.New("pgxdb: pool durations must not be negative")
	}

	return nil
}

func validateParsedPoolConfig(cfg *pgxpool.Config) error {
	if cfg.MaxConns <= 0 || cfg.MinConns < 0 || cfg.MinIdleConns < 0 || cfg.ConnConfig.ConnectTimeout < 0 {
		return errors.New("pgxdb: invalid parsed pool bounds")
	}

	if cfg.HealthCheckPeriod <= 0 {
		return errors.New("pgxdb: pool health check period must be positive")
	}

	return validatePoolDurations(PoolConfig{
		ConnMaxLifetime:       cfg.MaxConnLifetime,
		ConnMaxLifetimeJitter: cfg.MaxConnLifetimeJitter,
		ConnMaxIdleTime:       cfg.MaxConnIdleTime,
		PingTimeout:           cfg.PingTimeout,
	})
}

func applyConnectionOptions(cfg *pgxpool.Config, opts Options) {
	cfg.AfterConnect = opts.AfterConnect
	if len(opts.RuntimeParams) != 0 {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = make(map[string]string, len(opts.RuntimeParams))
		}

		maps.Copy(cfg.ConnConfig.RuntimeParams, opts.RuntimeParams)
	}

	if opts.CancelRequest != nil {
		policy := *opts.CancelRequest

		cfg.ConnConfig.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
			return &pgconn.CancelRequestContextWatcherHandler{
				Conn:               conn,
				CancelRequestDelay: policy.CancelRequestDelay,
				DeadlineDelay:      policy.DeadlineDelay,
			}
		}
	}
}
