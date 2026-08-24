package pgxdb

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

var queryExecModeNames = map[string]string{
	"cache_statement": "cache_statement",
	"cache_describe":  "cache_describe",
	"describe_exec":   "describe_exec",
	"exec":            "exec",
	"simple_protocol": "simple_protocol",
}

const dsnASCIISpaces = " \t\n\r\v\f"

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
		return errors.New("pgxdb: config is nil")
	}

	if c.SocketPath == "" && c.Host == "" {
		return errors.New("pgxdb: host or socket path is required")
	}

	if strings.TrimSpace(c.SSLMode) == "" {
		return errors.New("pgxdb: sslmode is required (no default; caller must set it explicitly)")
	}

	if c.QueryExecMode != "" && normalizeQueryExecMode(c.QueryExecMode) == "" {
		return fmt.Errorf("pgxdb: invalid query exec mode %q (allowed: cache_statement, cache_describe, describe_exec, exec, simple_protocol)", c.QueryExecMode)
	}

	return nil
}

func (c *Config) DSN() (string, error) {
	if err := c.Validate(); err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	return c.buildDSN(c.Password), nil
}

func (c *Config) SafeDSN() (string, error) {
	if err := c.Validate(); err != nil {
		return "", fmt.Errorf("validate: %w", err)
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

func validateExplicitSSLMode(rawDSN string) error {
	values, err := explicitSSLModeValues(rawDSN)
	if err != nil {
		return fmt.Errorf("explicit SSL mode values: %w", err)
	}

	if len(values) == 0 {
		return errors.New("pgxdb: sslmode is required in dsn (no implicit default)")
	}

	if len(values) != 1 {
		return errors.New("pgxdb: sslmode must be specified exactly once in dsn")
	}

	if strings.TrimSpace(values[0]) == "" {
		return errors.New("pgxdb: sslmode must not be empty in dsn")
	}

	return nil
}

func explicitSSLModeValues(rawDSN string) ([]string, error) {
	if strings.HasPrefix(rawDSN, "postgres://") || strings.HasPrefix(rawDSN, "postgresql://") {
		parsed, err := url.Parse(rawDSN)
		if err != nil {
			return nil, fmt.Errorf("pgxdb: parse dsn URL: %w", err)
		}

		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return nil, fmt.Errorf("pgxdb: parse dsn query: %w", err)
		}

		return query["sslmode"], nil
	}

	out, err := keywordSSLModeValues(rawDSN)
	if err != nil {
		return out, fmt.Errorf("keyword SSL mode values: %w", err)
	}

	return out, nil
}

func keywordSSLModeValues(rawDSN string) ([]string, error) {
	remaining := strings.TrimLeft(rawDSN, dsnASCIISpaces)

	var values []string

	for remaining != "" {
		equals := strings.IndexByte(remaining, '=')
		if equals < 0 {
			return nil, errors.New("pgxdb: invalid keyword/value dsn")
		}

		key := strings.Trim(remaining[:equals], dsnASCIISpaces)
		if key == "" {
			return nil, errors.New("pgxdb: invalid keyword/value dsn")
		}

		value, rest, err := consumeKeywordDSNValue(strings.TrimLeft(remaining[equals+1:], dsnASCIISpaces))
		if err != nil {
			return nil, fmt.Errorf("consume keyword DSN value: %w", err)
		}

		if key == "sslmode" {
			values = append(values, value)
		}

		remaining = rest
	}

	return values, nil
}

func consumeKeywordDSNValue(input string) (string, string, error) {
	if input == "" {
		return "", "", nil
	}

	quoted := input[0] == '\''
	start := 0

	if quoted {
		start = 1
	}

	var value strings.Builder

	for i := start; i < len(input); i++ {
		if input[i] == '\\' {
			i++
			if i == len(input) {
				return "", "", errors.New("pgxdb: invalid backslash in keyword/value dsn")
			}

			value.WriteByte(input[i])

			continue
		}

		if quoted {
			if input[i] == '\'' {
				return value.String(), strings.TrimLeft(input[i+1:], dsnASCIISpaces), nil
			}

			value.WriteByte(input[i])

			continue
		}

		if strings.ContainsRune(dsnASCIISpaces, rune(input[i])) {
			return value.String(), strings.TrimLeft(input[i:], dsnASCIISpaces), nil
		}

		value.WriteByte(input[i])
	}

	if quoted {
		return "", "", errors.New("pgxdb: unterminated quoted value in dsn")
	}

	return value.String(), "", nil
}

type PoolConfig struct {
	MinConns              int
	MaxConns              int
	ConnMaxLifetime       time.Duration
	ConnMaxLifetimeJitter time.Duration
	ConnMaxIdleTime       time.Duration
	// HealthCheckPeriod가 0 이하면 pgx가 채운 값(ParseConfig 기본 1분)을 그대로 둔다.
	// pgxpool은 이 값으로 time.NewTicker를 만들므로 0을 pgxpool.Config에 대입하면 panic한다.
	HealthCheckPeriod time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		// MinConns 0은 pgx 기본값이다. withPoolDefaults의 <=0 sentinel이 소비자가 명시한
		// 0(풀 최소 크기 없음)을 다시 기본값으로 덮어쓰지 않으려면 여기서도 0이어야 한다.
		MinConns:        0,
		MaxConns:        20,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	}
}

type PingConfig struct {
	PingTimeout time.Duration
}

func DefaultPingConfig() PingConfig {
	return PingConfig{
		PingTimeout: 5 * time.Second,
	}
}
