package pgxdb

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mustParse(t *testing.T, dsn string) *pgxpool.Config {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig(%q) error = %v", dsn, err)
	}

	return cfg
}

func TestOptionsPingTimeout(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want time.Duration
	}{
		{"default options", DefaultOptions(), 5 * time.Second},
		{"zero value", Options{}, 5 * time.Second},
		{"explicit", Options{Ping: PingConfig{PingTimeout: 250 * time.Millisecond}}, 250 * time.Millisecond},
		{"negative", Options{Ping: PingConfig{PingTimeout: -1}}, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.pingTimeout(); got != tt.want {
				t.Errorf("pingTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyPoolConfig_SetsAllFields(t *testing.T) {
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")

	err := applyPoolConfig(pc, PoolConfig{
		MinConns:              3,
		MaxConns:              12,
		ConnMaxLifetime:       2 * time.Hour,
		ConnMaxLifetimeJitter: 10 * time.Minute,
		ConnMaxIdleTime:       15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("applyPoolConfig error = %v", err)
	}

	if pc.MinConns != 3 || pc.MaxConns != 12 {
		t.Errorf("conns = %d/%d, want 3/12", pc.MinConns, pc.MaxConns)
	}

	if pc.MaxConnLifetime != 2*time.Hour || pc.MaxConnLifetimeJitter != 10*time.Minute || pc.MaxConnIdleTime != 15*time.Minute {
		t.Errorf("lifetimes = %v/%v/%v", pc.MaxConnLifetime, pc.MaxConnLifetimeJitter, pc.MaxConnIdleTime)
	}
}

func TestOverlayPoolConfig_PreservesUnsetFields(t *testing.T) {
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_max_conns=7")
	if pc.MaxConns != 7 {
		t.Fatalf("precondition: parsed MaxConns = %d, want 7", pc.MaxConns)
	}

	err := overlayPoolConfig(pc, PoolConfig{ConnMaxLifetime: time.Hour})
	if err != nil {
		t.Fatalf("overlayPoolConfig error = %v", err)
	}

	if pc.MaxConns != 7 {
		t.Errorf("MaxConns = %d, overlay must preserve parsed value when zero", pc.MaxConns)
	}

	if pc.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 1h", pc.MaxConnLifetime)
	}

	if pc.MaxConnLifetimeJitter != time.Hour/5 {
		t.Errorf("jitter = %v, want derived lifetime/5", pc.MaxConnLifetimeJitter)
	}
}

func TestOverlayPoolConfig_OverridesWhenSet(t *testing.T) {
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
	if err := overlayPoolConfig(pc, PoolConfig{MinConns: 4, MaxConns: 9, ConnMaxIdleTime: 5 * time.Minute}); err != nil {
		t.Fatalf("overlayPoolConfig error = %v", err)
	}

	if pc.MinConns != 4 || pc.MaxConns != 9 || pc.MaxConnIdleTime != 5*time.Minute {
		t.Errorf("got %d/%d/%v", pc.MinConns, pc.MaxConns, pc.MaxConnIdleTime)
	}
}

// QueryExecMode는 DSN 파라미터 단일 경로로만 적용된다. Pgx가 default_query_exec_mode 지원을
// 끊으면 이 테스트가 먼저 실패해야 한다.
func TestBuildConfigPool_AppliesQueryExecModeThroughDSNOnly(t *testing.T) {
	clearRootCertEnv(t)

	tests := []struct {
		name string
		mode string
		want pgx.QueryExecMode
	}{
		{name: "unset falls back to pgx default", mode: "", want: pgx.QueryExecModeCacheStatement},
		{name: "simple protocol", mode: "simple_protocol", want: pgx.QueryExecModeSimpleProtocol},
		{name: "normalized case", mode: "  EXEC  ", want: pgx.QueryExecModeExec},
		{name: "describe exec", mode: "describe_exec", want: pgx.QueryExecModeDescribeExec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Host: "127.0.0.1", Port: 5432, User: "u", Name: "db", SSLMode: testDisable, QueryExecMode: tt.mode}

			poolCfg, err := buildConfigPool(&cfg, Options{}.withDefaults())
			if err != nil {
				t.Fatalf("buildConfigPool error = %v", err)
			}

			if got := poolCfg.ConnConfig.DefaultQueryExecMode; got != tt.want {
				t.Errorf("DefaultQueryExecMode = %v, want %v", got, tt.want)
			}

			if _, ok := poolCfg.ConnConfig.RuntimeParams["default_query_exec_mode"]; ok {
				t.Error("default_query_exec_mode leaked into RuntimeParams (would be sent to the server)")
			}
		})
	}
}

func TestBuildConfigPool_RejectsInvalidQueryExecMode(t *testing.T) {
	clearRootCertEnv(t)

	cfg := Config{Host: "127.0.0.1", Port: 5432, User: "u", Name: "db", SSLMode: testDisable, QueryExecMode: "nope"}
	if _, err := buildConfigPool(&cfg, Options{}.withDefaults()); err == nil {
		t.Fatal("buildConfigPool with invalid query exec mode: expected error, got nil")
	}
}

func TestApplyPoolConfig_HealthCheckPeriod(t *testing.T) {
	t.Run("explicit value applied", func(t *testing.T) {
		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
		if err := applyPoolConfig(pc, withPoolDefaults(PoolConfig{HealthCheckPeriod: 15 * time.Second})); err != nil {
			t.Fatalf("applyPoolConfig error = %v", err)
		}

		if pc.HealthCheckPeriod != 15*time.Second {
			t.Errorf("HealthCheckPeriod = %v, want 15s", pc.HealthCheckPeriod)
		}
	})

	// 0을 그대로 대입하면 pgxpool이 time.NewTicker(0)로 panic한다.
	t.Run("zero preserves parsed default", func(t *testing.T) {
		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
		parsed := pc.HealthCheckPeriod

		if parsed <= 0 {
			t.Fatalf("precondition: parsed HealthCheckPeriod = %v, want positive pgx default", parsed)
		}

		if err := applyPoolConfig(pc, withPoolDefaults(PoolConfig{})); err != nil {
			t.Fatalf("applyPoolConfig error = %v", err)
		}

		if pc.HealthCheckPeriod != parsed {
			t.Errorf("HealthCheckPeriod = %v, want preserved %v", pc.HealthCheckPeriod, parsed)
		}
	})

	t.Run("overlay honors dsn value when unset", func(t *testing.T) {
		pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable&pool_health_check_period=42s")
		if pc.HealthCheckPeriod != 42*time.Second {
			t.Fatalf("precondition: parsed HealthCheckPeriod = %v, want 42s", pc.HealthCheckPeriod)
		}

		if err := overlayPoolConfig(pc, PoolConfig{MaxConns: 3}); err != nil {
			t.Fatalf("overlayPoolConfig error = %v", err)
		}

		if pc.HealthCheckPeriod != 42*time.Second {
			t.Errorf("HealthCheckPeriod = %v, want preserved 42s", pc.HealthCheckPeriod)
		}

		if err := overlayPoolConfig(pc, PoolConfig{MaxConns: 3, HealthCheckPeriod: 7 * time.Second}); err != nil {
			t.Fatalf("overlayPoolConfig error = %v", err)
		}

		if pc.HealthCheckPeriod != 7*time.Second {
			t.Errorf("HealthCheckPeriod = %v, want overridden 7s", pc.HealthCheckPeriod)
		}
	})
}

func TestValidateConnCounts_RejectsInvertedRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pool    PoolConfig
		wantErr bool
	}{
		{name: "min below max", pool: PoolConfig{MinConns: 2, MaxConns: 10}},
		{name: "min equals max", pool: PoolConfig{MinConns: 10, MaxConns: 10}},
		{name: "min unset with max set", pool: PoolConfig{MaxConns: 10}},
		{name: "max unset means overlay keeps parsed value", pool: PoolConfig{MinConns: 30}},
		{name: "both unset", pool: PoolConfig{}},
		{name: "inverted", pool: PoolConfig{MinConns: 30, MaxConns: 10}, wantErr: true},
		{name: "negative min", pool: PoolConfig{MinConns: -1, MaxConns: 10}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateConnCounts(tt.pool)
			if tt.wantErr != (err != nil) {
				t.Fatalf("validateConnCounts(%+v) error = %v, wantErr = %v", tt.pool, err, tt.wantErr)
			}
		})
	}
}

func TestApplyAndOverlayPoolConfig_PropagateInvertedRangeError(t *testing.T) {
	t.Parallel()

	inverted := PoolConfig{MinConns: 30, MaxConns: 10}
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")

	if err := applyPoolConfig(pc, inverted); err == nil {
		t.Fatal("applyPoolConfig with inverted conn range: expected error, got nil")
	}

	if err := overlayPoolConfig(pc, inverted); err == nil {
		t.Fatal("overlayPoolConfig with inverted conn range: expected error, got nil")
	}

	if pc.MinConns == 30 {
		t.Error("rejected config must not be partially applied")
	}
}

func TestOpenPool_RejectsInvalidConfig(t *testing.T) {
	clearRootCertEnv(t)

	_, err := OpenPool(t.Context(), Config{Host: "h"}, Options{})
	if err == nil {
		t.Fatal("OpenPool with empty sslmode: expected error, got nil")
	}

	if !strings.Contains(err.Error(), "sslmode") {
		t.Errorf("error = %v, want sslmode requirement", err)
	}
}

func TestOpenPoolDSN_RejectsEmpty(t *testing.T) {
	if _, err := OpenPoolDSN(t.Context(), "   ", Options{}); err == nil {
		t.Fatal("OpenPoolDSN with blank dsn: expected error, got nil")
	}
}

func TestOpenPoolDSNNormalizesLeadingWhitespaceBeforeValidationAndParse(t *testing.T) {
	t.Parallel()

	_, err := OpenPoolDSN(t.Context(), "  postgres://user:password@127.0.0.1:1/app?sslmode=disable", Options{})
	if err == nil || strings.Contains(err.Error(), "sslmode is required") || strings.Contains(err.Error(), "parse dsn") {
		t.Fatalf("OpenPoolDSN leading space error = %v, want post-parse connection failure", err)
	}
}

func TestOpenPoolDSN_RejectsMissingSSLModeBeforeConnect(t *testing.T) {
	_, err := OpenPoolDSN(t.Context(), "postgres://user@127.0.0.1:1/app", Options{})
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("OpenPoolDSN missing sslmode error = %v, want explicit sslmode error", err)
	}

	if strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "ping") {
		t.Fatalf("OpenPoolDSN missing sslmode error = %v, want validation before connect", err)
	}
}
