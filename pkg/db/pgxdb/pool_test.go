package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const loopbackHost = "127.0.0.1"

func mustParse(t *testing.T, dsn string) *pgxpool.Config {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig(%q) error = %v", dsn, err)
	}
	return cfg
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

func TestShouldFallbackToLocalhost(t *testing.T) {
	dnsErr := &net.DNSError{Name: "postgres", Err: "no such host", IsNotFound: true}
	tests := []struct {
		name string
		err  error
		host string
		want bool
	}{
		{name: "nil error", err: nil, host: "postgres", want: false},
		{name: "postgres dns error", err: dnsErr, host: "postgres", want: true},
		{name: "localhost not eligible", err: dnsErr, host: "localhost", want: false},
		{name: "other host not eligible", err: &net.DNSError{Name: "db.internal", Err: "no such host"}, host: "db.internal", want: false},
		{name: "string form", err: errors.New("lookup postgres: no such host"), host: "postgres", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldFallbackToLocalhost(tt.err, tt.host); got != tt.want {
				t.Errorf("ShouldFallbackToLocalhost(%v, %q) = %v, want %v", tt.err, tt.host, got, tt.want)
			}
		})
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

func TestOpenPoolWithRetry_ExhaustsAttempts(t *testing.T) {
	clearRootCertEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := Config{Host: loopbackHost, Port: 59999, User: "u", Name: "db", SSLMode: "disable"}
	opts := Options{Retry: RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, PingTimeout: 300 * time.Millisecond}}

	_, err := OpenPoolWithRetry(ctx, cfg, opts)
	if err == nil {
		t.Fatal("expected error connecting to closed port, got nil")
	}
	if !strings.Contains(err.Error(), "after retries") {
		t.Errorf("error = %v, want retry-exhaustion wrapping", err)
	}
}

func TestConnectRetryDelay_UsesHalfJitter(t *testing.T) {
	tests := []struct {
		name     string
		computed time.Duration
		maxDelay time.Duration
		cap      time.Duration
	}{
		{name: "below cap", computed: 2 * time.Second, maxDelay: 30 * time.Second, cap: 2 * time.Second},
		{name: "above cap", computed: 32 * time.Second, maxDelay: 30 * time.Second, cap: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 1000 {
				got := connectRetryDelay(tt.computed, tt.maxDelay)
				if got < tt.cap/2 || got >= tt.cap {
					t.Fatalf("connectRetryDelay(%v, %v) = %v, want in [%v, %v)", tt.computed, tt.maxDelay, got, tt.cap/2, tt.cap)
				}
			}
		})
	}
}

func TestIsRetryableConnectError_Classification(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "auth invalid_password 28P01", ctx: live, err: &pgconn.PgError{Code: sqlstateInvalidPassword}, want: false},
		{name: "auth invalid_authorization 28000", ctx: live, err: &pgconn.PgError{Code: sqlstateInvalidAuthorization}, want: false},
		{name: "wrapped auth 28P01", ctx: live, err: fmt.Errorf("pgxdb: ping: %w", &pgconn.PgError{Code: sqlstateInvalidPassword}), want: false},
		{name: "db not exist 3D000 retryable", ctx: live, err: &pgconn.PgError{Code: "3D000"}, want: true},
		{name: "connection refused retryable", ctx: live, err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}, want: true},
		{name: "generic error retryable", ctx: live, err: errors.New("temporary network glitch"), want: true},
		{name: "cancelled context permanent", ctx: cancelled, err: &pgconn.PgError{Code: "3D000"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableConnectError(tt.ctx, tt.err); got != tt.want {
				t.Errorf("isRetryableConnectError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestOpenPoolWithRetry_ValidationFailsFast(t *testing.T) {
	clearRootCertEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := Config{Host: loopbackHost, Port: 59999, User: "u", Name: "db"}
	opts := Options{Retry: RetryConfig{MaxAttempts: 5, BaseDelay: time.Hour, MaxDelay: time.Hour}}

	start := time.Now()
	_, err := OpenPoolWithRetry(ctx, cfg, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "sslmode") {
		t.Errorf("error = %v, want sslmode validation error", err)
	}
	if strings.Contains(err.Error(), "after retries") || strings.Contains(err.Error(), "retry aborted") {
		t.Errorf("error = %v, want pre-loop return without entering the retry loop", err)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, want fail-fast (BaseDelay=1h would dominate if the loop ran)", elapsed)
	}
}

func TestOpenPoolWithRetry_ConnCountFailsFast(t *testing.T) {
	clearRootCertEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := Config{Host: loopbackHost, Port: 59999, User: "u", Name: "db", SSLMode: "disable"}
	opts := Options{
		Pool:  PoolConfig{MaxConns: math.MaxInt32 + 1},
		Retry: RetryConfig{MaxAttempts: 5, BaseDelay: time.Hour, MaxDelay: time.Hour},
	}

	start := time.Now()
	_, err := OpenPoolWithRetry(ctx, cfg, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected int32-range validation error, got nil")
	}
	if !strings.Contains(err.Error(), "int32 range") {
		t.Errorf("error = %v, want int32-range validation error", err)
	}
	if strings.Contains(err.Error(), "after retries") {
		t.Errorf("error = %v, want pre-loop return", err)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, want fail-fast", elapsed)
	}
}

func TestOpenPoolWithRetry_ContextCancelledFailsFast(t *testing.T) {
	clearRootCertEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := Config{Host: loopbackHost, Port: 59999, User: "u", Name: "db", SSLMode: "disable"}
	opts := Options{Retry: RetryConfig{MaxAttempts: 5, BaseDelay: time.Hour, MaxDelay: time.Hour}}

	start := time.Now()
	_, err := OpenPoolWithRetry(ctx, cfg, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want errors.Is(context.Canceled)", err)
	}
	if strings.Contains(err.Error(), "after retries") {
		t.Errorf("error = %v, want permanent-path wrapping without 'after retries'", err)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, want fail-fast on cancelled context", elapsed)
	}
}
