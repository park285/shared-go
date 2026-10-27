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

// safeDSN은 Validate를 통과한 Config의 DSN을 비밀번호만 가린 형태로 만든다. 파싱 오류에 원문
// 비밀번호가 실리지 않도록 pgx 파싱은 이 값으로 하고 비밀번호는 파싱 뒤 따로 채운다.
func (c *Config) safeDSN() (string, error) {
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

// NormalizeSSLMode는 명시된 지원 모드의 대소문자와 주변 공백을 정규화한다.
// 배포 환경의 TLS 정책은 서비스가 강제한다.
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

// ParseConfig는 sslmode를 명시한 단일 연결 DSN을 정규화해 파싱한다.
// 배포별 TLS 요건은 호출자가 책임진다. 자격 증명이 하위 오류에 포함될 수 있어
// 파싱 실패 시 원인 오류를 노출하지 않는다.
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

		// pgx처럼 디코딩 전 ASCII 공백만 제거하고 '+'를 보존한다.
		// 디코딩된 공백 때문에 다른 키가 sslmode로 인식되어서는 안 된다.
		key, err := url.PathUnescape(strings.Trim(key, " "))
		if err != nil {
			return "", errors.New("pgxdb: parse dsn query: invalid key")
		}

		if key == "sslmode" {
			if mode != "" {
				return "", errors.New("pgxdb: sslmode must be specified exactly once in dsn (no implicit default)")
			}

			// sslmode 값만 폼 인코딩 공백을 정규화하고 나머지는 pgx 해석을 따른다.
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

	// 모드만 바꿔 다른 옵션·자격 증명의 쿼리 경계, 세미콜론, '+', 인코딩 바이트를 보존한다.
	return rawDSN[:valueStart] + mode + rawDSN[valueEnd:], nil
}

// urlDSNQueryStart는 pgx v5의 사용자 정보·호스트 목록·포트·경로 경계만 따른다.
// 전체 URI 검증은 pgx가 담당하며 쿼리를 찾지 못하면 -1을 반환한다.
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

	// sslmode만 바꾸고 다른 옵션과 이스케이프된 자격 증명을 보존한다.
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
	// 0이면 pgx가 파싱한 상태 검사 주기를 유지하며 음수는 허용하지 않는다.
	HealthCheckPeriod time.Duration
	// PingTimeout은 시작 시 Ping이 아닌 획득 시 상태 검사를 제한한다.
	// 0이면 pgx/DSN에서 파싱한 값을 유지한다.
	PingTimeout time.Duration
}

// defaultPoolConfig는 구조체 경로에서 0인 수명 필드를 채울 기본값이다.
func defaultPoolConfig() PoolConfig {
	return PoolConfig{
		// 유휴 상태에서 연결을 하나도 유지하지 않아도 된다.
		MinConns:        0,
		MaxConns:        20,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	}
}

type PingConfig struct {
	PingTimeout time.Duration
}
