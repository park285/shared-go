package pgxdb

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestApplyQueryExecMode(t *testing.T) {
	pc := mustParse(t, "postgres://u@127.0.0.1:5432/db?sslmode=disable")
	if err := applyQueryExecMode(pc, ""); err != nil {
		t.Fatalf("empty mode: unexpected error %v", err)
	}
	if err := applyQueryExecMode(pc, "simple_protocol"); err != nil {
		t.Fatalf("valid mode: unexpected error %v", err)
	}
	if err := applyQueryExecMode(pc, "nope"); err == nil {
		t.Fatal("invalid mode: expected error, got nil")
	}
}

func TestOpenPool_RejectsInvalidConfig(t *testing.T) {
	clearRootCertEnv(t)
	_, err := OpenPool(context.Background(), Config{Host: "h"}, Options{})
	if err == nil {
		t.Fatal("OpenPool with empty sslmode: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sslmode") {
		t.Errorf("error = %v, want sslmode requirement", err)
	}
}

func TestOpenPoolDSN_RejectsEmpty(t *testing.T) {
	if _, err := OpenPoolDSN(context.Background(), "   ", Options{}); err == nil {
		t.Fatal("OpenPoolDSN with blank dsn: expected error, got nil")
	}
}

func TestOpenPoolDSNNormalizesLeadingWhitespaceBeforeValidationAndParse(t *testing.T) {
	t.Parallel()
	_, err := OpenPoolDSN(context.Background(), "  postgres://user:password@127.0.0.1:1/app?sslmode=disable", Options{})
	if err == nil || strings.Contains(err.Error(), "sslmode is required") || strings.Contains(err.Error(), "parse dsn") {
		t.Fatalf("OpenPoolDSN leading space error = %v, want post-parse connection failure", err)
	}
}

func TestOpenPoolDSN_RejectsMissingSSLModeBeforeConnect(t *testing.T) {
	_, err := OpenPoolDSN(context.Background(), "postgres://user@127.0.0.1:1/app", Options{})
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("OpenPoolDSN missing sslmode error = %v, want explicit sslmode error", err)
	}
	if strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "ping") {
		t.Fatalf("OpenPoolDSN missing sslmode error = %v, want validation before connect", err)
	}
}
