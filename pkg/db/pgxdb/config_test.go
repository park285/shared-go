package pgxdb

import (
	"math"
	"strings"
	"testing"
	"time"
)

func clearRootCertEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_SSLROOTCERT", "")
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{name: "nil", cfg: nil, wantErr: true},
		{name: "no host or socket", cfg: &Config{SSLMode: "disable"}, wantErr: true},
		{name: "empty sslmode", cfg: &Config{Host: "localhost", SSLMode: ""}, wantErr: true},
		{name: "blank sslmode", cfg: &Config{Host: "localhost", SSLMode: "   "}, wantErr: true},
		{name: "invalid query exec mode", cfg: &Config{Host: "localhost", SSLMode: "disable", QueryExecMode: "bogus"}, wantErr: true},
		{name: "valid tcp", cfg: &Config{Host: "localhost", SSLMode: "verify-full"}, wantErr: false},
		{name: "valid socket", cfg: &Config{SocketPath: "/var/run/postgresql", SSLMode: "disable"}, wantErr: false},
		{name: "valid query exec mode", cfg: &Config{Host: "localhost", SSLMode: "disable", QueryExecMode: "simple_protocol"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigDSN_RequiresSSLMode(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{Host: "localhost", Port: 5432, User: "u", Password: "p", Name: "db"}
	if _, err := cfg.DSN(); err == nil {
		t.Fatal("DSN() with empty sslmode: expected error, got nil")
	}
	if _, err := cfg.SafeDSN(); err == nil {
		t.Fatal("SafeDSN() with empty sslmode: expected error, got nil")
	}
}

func TestValidateExplicitSSLMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{name: "URL omitted", dsn: "postgres://user@db.example/app", wantErr: true},
		{name: "URL empty", dsn: "postgres://user@db.example/app?sslmode=", wantErr: true},
		{name: "URL encoded blank", dsn: "postgres://user@db.example/app?sslmode=%20", wantErr: true},
		{name: "URL encoded verify full", dsn: "postgres://user@db.example/app?sslmode=verify%2Dfull"},
		{name: "URL complete verify full", dsn: "postgres://user@db.example/app?sslmode=verify-full&sslrootcert=%2Frun%2Fca.pem"},
		{name: "URL duplicate same", dsn: "postgres://user@db.example/app?sslmode=disable&sslmode=disable", wantErr: true},
		{name: "URL duplicate conflicting", dsn: "postgres://user@db.example/app?sslmode=disable&sslmode=verify-full", wantErr: true},
		{name: "keyword omitted", dsn: "host=db.example user=app", wantErr: true},
		{name: "keyword empty", dsn: "host=db.example sslmode=''", wantErr: true},
		{name: "keyword spaced quoted", dsn: "host = 'db.example' sslmode = 'verify-full'"},
		{name: "keyword explicit disable", dsn: "host=db.example sslmode=disable"},
		{name: "keyword duplicate same", dsn: "sslmode=disable host=db.example sslmode='disable'", wantErr: true},
		{name: "keyword duplicate conflicting", dsn: "sslmode=disable host=db.example sslmode=verify-full", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExplicitSSLMode(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExplicitSSLMode(%q) error = %v, wantErr %v", tt.dsn, err, tt.wantErr)
			}
		})
	}
}

func TestConfigDSN_TCP(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{Host: "db.example", Port: 6432, User: "svc", Password: "test-dsn-placeholder", Name: "app", SSLMode: "verify-full"}
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	for _, want := range []string{"host='db.example'", "port=6432", "user='svc'", "password='test-dsn-placeholder'", "dbname='app'", "sslmode='verify-full'"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN() = %q, missing %q", dsn, want)
		}
	}
	if strings.Contains(dsn, "sslrootcert") {
		t.Errorf("DSN() = %q, should omit sslrootcert when unset", dsn)
	}
}

func TestConfigDSN_Socket(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{SocketPath: "/var/run/postgresql", User: "u", Name: "db", SSLMode: "disable"}
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(dsn, "host='/var/run/postgresql'") {
		t.Errorf("DSN() = %q, missing socket host", dsn)
	}
	if strings.Contains(dsn, "port=") {
		t.Errorf("DSN() = %q, should omit port for socket mode", dsn)
	}
}

func TestConfigDSN_RootCertAndQueryExecMode(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{Host: "h", SSLMode: "verify-full", SSLRootCert: "/etc/ssl/ca.pem", QueryExecMode: "SIMPLE_PROTOCOL"}
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(dsn, "sslrootcert='/etc/ssl/ca.pem'") {
		t.Errorf("DSN() = %q, missing sslrootcert", dsn)
	}
	if !strings.Contains(dsn, "default_query_exec_mode='simple_protocol'") {
		t.Errorf("DSN() = %q, missing normalized query exec mode", dsn)
	}
}

func TestConfigDSN_RootCertEnvFallback(t *testing.T) {
	t.Setenv("POSTGRES_SSLROOTCERT", "/env/ca.pem")
	cfg := &Config{Host: "h", SSLMode: "verify-full"}
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(dsn, "sslrootcert='/env/ca.pem'") {
		t.Errorf("DSN() = %q, missing env-sourced sslrootcert", dsn)
	}
}

func TestSafeDSN_MasksPassword(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{Host: "h", SSLMode: "disable", User: "u", Password: "test-redaction-placeholder", Name: "db"}
	safe, err := cfg.SafeDSN()
	if err != nil {
		t.Fatalf("SafeDSN() error = %v", err)
	}
	if strings.Contains(safe, "test-redaction-placeholder") {
		t.Errorf("SafeDSN() = %q, leaks password", safe)
	}
	if !strings.Contains(safe, "password='***'") {
		t.Errorf("SafeDSN() = %q, missing masked password", safe)
	}

	full, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(full, "password='test-redaction-placeholder'") {
		t.Errorf("DSN() = %q, missing real password", full)
	}
}

func TestLibpqQuote_Escaping(t *testing.T) {
	clearRootCertEnv(t)
	cfg := &Config{Host: "h", SSLMode: "disable", Password: `pa'ss\word`}
	dsn, err := cfg.DSN()
	if err != nil {
		t.Fatalf("DSN() error = %v", err)
	}
	if !strings.Contains(dsn, `password='pa\'ss\\word'`) {
		t.Errorf("DSN() = %q, quote/backslash not escaped", dsn)
	}
}

func TestDefaultPoolConfig_EnvOverrideAndClamp(t *testing.T) {
	t.Setenv("DB_POOL_MIN_CONNS", "0")
	t.Setenv("DB_POOL_MAX_CONNS", "500")
	pc := DefaultPoolConfig()
	if pc.MinConns != 1 {
		t.Errorf("MinConns = %d, want clamp to 1", pc.MinConns)
	}
	if pc.MaxConns != 200 {
		t.Errorf("MaxConns = %d, want clamp to 200", pc.MaxConns)
	}
	if pc.ConnMaxLifetime != time.Hour {
		t.Errorf("ConnMaxLifetime = %v, want 1h", pc.ConnMaxLifetime)
	}
}

func TestWithPoolDefaults_SharesSourceWithDefaultPoolConfig(t *testing.T) {
	t.Setenv("DB_POOL_MIN_CONNS", "7")
	t.Setenv("DB_POOL_MAX_CONNS", "33")

	def := DefaultPoolConfig()
	got := withPoolDefaults(PoolConfig{})

	if got.MinConns != def.MinConns || got.MaxConns != def.MaxConns {
		t.Errorf("conns = %d/%d, want %d/%d (single source: DefaultPoolConfig)", got.MinConns, got.MaxConns, def.MinConns, def.MaxConns)
	}
	if got.MinConns != 7 || got.MaxConns != 33 {
		t.Errorf("conns = %d/%d, want env-tuned 7/33", got.MinConns, got.MaxConns)
	}
	if got.ConnMaxLifetime != def.ConnMaxLifetime || got.ConnMaxIdleTime != def.ConnMaxIdleTime {
		t.Errorf("lifetimes = %v/%v, want %v/%v", got.ConnMaxLifetime, got.ConnMaxIdleTime, def.ConnMaxLifetime, def.ConnMaxIdleTime)
	}
	if got.ConnMaxLifetimeJitter != got.ConnMaxLifetime/5 {
		t.Errorf("jitter = %v, want lifetime/5", got.ConnMaxLifetimeJitter)
	}
}

func TestWithPoolDefaults_PreservesExplicitValues(t *testing.T) {
	t.Setenv("DB_POOL_MIN_CONNS", "7")
	t.Setenv("DB_POOL_MAX_CONNS", "33")

	got := withPoolDefaults(PoolConfig{MinConns: 3, MaxConns: 12, ConnMaxLifetime: 2 * time.Hour, ConnMaxLifetimeJitter: 90 * time.Second, ConnMaxIdleTime: 5 * time.Minute})
	if got.MinConns != 3 || got.MaxConns != 12 {
		t.Errorf("conns = %d/%d, want explicit 3/12 (fallback must not override)", got.MinConns, got.MaxConns)
	}
	if got.ConnMaxLifetime != 2*time.Hour || got.ConnMaxLifetimeJitter != 90*time.Second || got.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("lifetimes = %v/%v/%v, want explicit values preserved", got.ConnMaxLifetime, got.ConnMaxLifetimeJitter, got.ConnMaxIdleTime)
	}
}

func TestValidateConnCounts_Int32Range(t *testing.T) {
	if err := validateConnCounts(PoolConfig{MinConns: 1, MaxConns: 20}); err != nil {
		t.Fatalf("valid counts: unexpected error %v", err)
	}
	if err := validateConnCounts(PoolConfig{MaxConns: math.MaxInt32 + 1}); err == nil {
		t.Fatal("max > int32: expected error, got nil")
	}
	if err := validateConnCounts(PoolConfig{MinConns: -1}); err == nil {
		t.Fatal("negative min: expected error, got nil")
	}
}
