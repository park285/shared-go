package pgxdb

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/envutil"
)

var queryExecModeNames = map[string]string{
	"cache_statement": "cache_statement",
	"cache_describe":  "cache_describe",
	"describe_exec":   "describe_exec",
	"exec":            "exec",
	"simple_protocol": "simple_protocol",
}

type Config struct {
	Host          string
	Port          int
	SocketPath    string
	User          string
	Password      string
	Name          string
	SSLMode       string
	SSLRootCert   string
	QueryExecMode string
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("pgxdb: config is nil")
	}
	if c.SocketPath == "" && c.Host == "" {
		return fmt.Errorf("pgxdb: host or socket path is required")
	}
	if strings.TrimSpace(c.SSLMode) == "" {
		return fmt.Errorf("pgxdb: sslmode is required (no default; caller must set it explicitly)")
	}
	if c.QueryExecMode != "" && normalizeQueryExecMode(c.QueryExecMode) == "" {
		return fmt.Errorf("pgxdb: invalid query exec mode %q (allowed: cache_statement, cache_describe, describe_exec, exec, simple_protocol)", c.QueryExecMode)
	}
	return nil
}

func (c *Config) DSN() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.buildDSN(c.Password), nil
}

func (c *Config) SafeDSN() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	password := c.Password
	if password != "" {
		password = "***"
	}
	return c.buildDSN(password), nil
}

func (c *Config) buildDSN(password string) string {
	sslRootCert := strings.TrimSpace(c.SSLRootCert)
	if sslRootCert == "" {
		sslRootCert = strings.TrimSpace(envutil.String("POSTGRES_SSLROOTCERT", ""))
	}
	queryExecMode := normalizeQueryExecMode(c.QueryExecMode)

	parts := make([]string, 0, 8)
	if c.SocketPath != "" {
		parts = append(parts, libpqKeywordValue("host", c.SocketPath))
	} else {
		parts = append(parts,
			libpqKeywordValue("host", c.Host),
			"port="+strconv.Itoa(c.Port),
		)
	}
	parts = append(parts,
		libpqKeywordValue("user", c.User),
		libpqKeywordValue("password", password),
		libpqKeywordValue("dbname", c.Name),
		libpqKeywordValue("sslmode", c.SSLMode),
	)
	if sslRootCert != "" {
		parts = append(parts, libpqKeywordValue("sslrootcert", sslRootCert))
	}
	if queryExecMode != "" {
		parts = append(parts, libpqKeywordValue("default_query_exec_mode", queryExecMode))
	}
	return strings.Join(parts, " ")
}

func libpqKeywordValue(key, value string) string {
	return key + "=" + libpqQuote(value)
}

func libpqQuote(value string) string {
	var builder strings.Builder
	builder.Grow(len(value) + 2)
	builder.WriteByte('\'')
	for _, char := range value {
		if char == '\\' || char == '\'' {
			builder.WriteByte('\\')
		}
		builder.WriteRune(char)
	}
	builder.WriteByte('\'')
	return builder.String()
}

func normalizeQueryExecMode(mode string) string {
	return queryExecModeNames[strings.ToLower(strings.TrimSpace(mode))]
}

type PoolConfig struct {
	MinConns              int
	MaxConns              int
	ConnMaxLifetime       time.Duration
	ConnMaxLifetimeJitter time.Duration
	ConnMaxIdleTime       time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MinConns:        clamp(envutil.Int("DB_POOL_MIN_CONNS", 5), 1, 100),
		MaxConns:        clamp(envutil.Int("DB_POOL_MAX_CONNS", 20), 1, 200),
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	}
}

func clamp(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	PingTimeout time.Duration
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   2 * time.Second,
		MaxDelay:    30 * time.Second,
		PingTimeout: 5 * time.Second,
	}
}
