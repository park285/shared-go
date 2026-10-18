package pgxdb

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

	if _, err := NormalizeSSLMode(c.SSLMode); err != nil {
		return err
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

// buildDSN은 sslrootcert를 Config.SSLRootCert에서만 채운다.
func (c *Config) buildDSN(password string) string {
	sslRootCert := strings.TrimSpace(c.SSLRootCert)
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
		libpqKeywordValue("sslmode", strings.ToLower(strings.TrimSpace(c.SSLMode))),
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

// NormalizeSSLMode requires an explicit supported mode and normalizes its case
// and surrounding whitespace. Services enforce their own deployment TLS policy.
func NormalizeSSLMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return mode, nil
	case "":
		return "", errors.New("pgxdb: sslmode is required (no implicit default)")
	default:
		return "", errors.New("pgxdb: invalid sslmode (allowed: disable, allow, prefer, require, verify-ca, verify-full)")
	}
}

// ParseConfig parses a single-connection DSN with an explicit, normalized sslmode.
// Deployment-specific TLS requirements remain the caller's responsibility.
// Parse failures deliberately omit the underlying error: parsers may include
// credentials even when their connection-string redaction succeeds.
func ParseConfig(rawDSN string) (*pgx.ConnConfig, error) {
	dsn, err := normalizeDSN(rawDSN)
	if err != nil {
		return nil, err
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("pgxdb: parse dsn: invalid connection configuration")
	}

	return cfg, nil
}

func normalizeDSN(rawDSN string) (string, error) {
	rawDSN = strings.TrimSpace(rawDSN)
	if rawDSN == "" {
		return "", errors.New("pgxdb: dsn is required")
	}

	if strings.HasPrefix(rawDSN, "postgres://") || strings.HasPrefix(rawDSN, "postgresql://") {
		return normalizeURLDSN(rawDSN)
	}

	return normalizeKeywordDSN(rawDSN)
}

func normalizeURLDSN(rawDSN string) (string, error) {
	queryOffset := urlDSNQueryStart(rawDSN)
	if queryOffset < 0 {
		return "", errors.New("pgxdb: sslmode is required in dsn (no implicit default)")
	}

	mode := ""
	valueStart, valueEnd := 0, 0

	for remaining := rawDSN[queryOffset:]; remaining != ""; {
		pair, rest, _ := strings.Cut(remaining, "&")
		key, value, hasValue := strings.Cut(pair, "=")

		if !hasValue || strings.Contains(value, "=") {
			return "", errors.New("pgxdb: parse dsn query: invalid query")
		}

		// pgx trims raw ASCII spaces before percent-decoding keys and keeps
		// literal '+'. Decoded spaces must not turn another key into sslmode.
		key, err := url.PathUnescape(strings.Trim(key, " "))
		if err != nil {
			return "", errors.New("pgxdb: parse dsn query: invalid key")
		}

		if key == "sslmode" {
			if mode != "" {
				return "", errors.New("pgxdb: sslmode must be specified exactly once in dsn (no implicit default)")
			}

			// Retain the existing form-encoded whitespace normalization for
			// this policy value only; other URI values follow pgx semantics.
			decoded, err := url.QueryUnescape(value)
			if err != nil {
				return "", errors.New("pgxdb: parse dsn query: invalid sslmode")
			}

			mode, err = NormalizeSSLMode(decoded)
			if err != nil {
				return "", err
			}

			valueStart = len(rawDSN) - len(remaining) + len(pair) - len(value)
			valueEnd = valueStart + len(value)
		}

		remaining = rest
	}

	if mode == "" {
		return "", errors.New("pgxdb: sslmode must be specified exactly once in dsn (no implicit default)")
	}

	if rawDSN[valueStart:valueEnd] == mode {
		return rawDSN, nil
	}

	// Replace only the mode value; preserve raw query boundaries, semicolons,
	// '+' and percent-encoded bytes in credentials and unrelated options.
	return rawDSN[:valueStart] + mode + rawDSN[valueEnd:], nil
}

// urlDSNQueryStart follows pgx v5's userinfo, host-list, port and path
// boundaries without interpreting the components. Full URI validation is pgx's
// responsibility; -1 means no query could be located at those boundaries.
func urlDSNQueryStart(rawDSN string) int {
	_, remaining, _ := strings.Cut(rawDSN, "://")
	if i := strings.IndexAny(remaining, "@/"); i >= 0 && remaining[i] == '@' {
		remaining = remaining[i+1:]
	}

	for {
		if strings.HasPrefix(remaining, "[") {
			var closed bool

			_, remaining, closed = strings.Cut(remaining, "]")

			if !closed {
				return -1
			}
		} else {
			i := strings.IndexAny(remaining, ":/?,")
			if i < 0 {
				return -1
			}

			remaining = remaining[i:]
		}

		if port, ok := strings.CutPrefix(remaining, ":"); ok {
			i := strings.IndexAny(port, "/?,")
			if i < 0 {
				return -1
			}

			remaining = port[i:]
		}

		nextHost, ok := strings.CutPrefix(remaining, ",")
		if !ok {
			break
		}

		remaining = nextHost
	}

	if strings.HasPrefix(remaining, "/") {
		i := strings.IndexByte(remaining, '?')
		if i < 0 {
			return -1
		}

		remaining = remaining[i:]
	}

	if !strings.HasPrefix(remaining, "?") {
		return -1
	}

	return len(rawDSN) - len(remaining) + 1
}

func normalizeKeywordDSN(rawDSN string) (string, error) {
	remaining := rawDSN
	mode := ""
	valueStart, valueEnd := 0, 0

	for remaining != "" {
		key, input, ok := strings.Cut(remaining, "=")
		if !ok || strings.Trim(key, dsnASCIISpaces) == "" {
			return "", errors.New("pgxdb: invalid keyword/value dsn")
		}

		input = strings.TrimLeft(input, dsnASCIISpaces)

		value, rest, err := consumeKeywordDSNValue(input)
		if err != nil {
			return "", err
		}

		if strings.Trim(key, dsnASCIISpaces) == "sslmode" {
			if mode != "" {
				return "", errors.New("pgxdb: sslmode must be specified exactly once in dsn")
			}

			mode, err = NormalizeSSLMode(value)
			if err != nil {
				return "", err
			}

			valueStart = len(rawDSN) - len(input)
			valueEnd = len(rawDSN) - len(rest)
		}

		remaining = rest
	}

	if mode == "" {
		return "", errors.New("pgxdb: sslmode is required in dsn (no implicit default)")
	}

	// Replace only sslmode; preserve all other options and escaped credentials.
	return rawDSN[:valueStart] + libpqQuote(mode) + " " + rawDSN[valueEnd:], nil
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
	// Zero preserves pgx's parsed health-check period; negative values are invalid.
	HealthCheckPeriod time.Duration
	// PingTimeout bounds acquire-time health pings, not the startup Ping budget.
	// Zero preserves the parsed pgx/DSN value.
	PingTimeout time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		// Zero explicitly permits an idle pool with no minimum connections.
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
