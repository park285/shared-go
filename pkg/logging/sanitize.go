package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

type sanitizeHandler struct {
	inner slog.Handler
}

func newSanitizeHandler(inner slog.Handler) *sanitizeHandler {
	return &sanitizeHandler{inner: inner}
}

// 민감 키 리스트 (case-insensitive 매칭)
var (
	bearerTokenRegex = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	querySecretRegex = regexp.MustCompile(`(?i)([?&;](?:key|api_key|apikey|token|password|pwd|passwd|client_secret|secret|private_key|secret_key)=)[^&\s]+`)
)

const (
	tokenAPIKey        = "api_key"
	tokenAPIKeyCompact = "apikey"
)

// querySecretTokens는 querySecretRegex가 매치할 수 있는 키 이름 집합으로,
// 정규식 실행 전 싼 substring pre-check 게이트에 쓰인다. 정규식이 매치하는
// 입력은 반드시 이 토큰 중 하나를 case-insensitive로 포함하므로 게이트는 안전하다.
var querySecretTokens = []string{
	"key", tokenAPIKey, tokenAPIKeyCompact, "token", "password", "pwd", "passwd",
	"client_secret", "secret", "private_key", "secret_key",
}

var sensitiveExactKeys = map[string]struct{}{
	"token":            {},
	"bot_token":        {},
	"access_token":     {},
	"refresh_token":    {},
	"password":         {},
	"pwd":              {},
	"passwd":           {},
	"secret":           {},
	"client_secret":    {},
	tokenAPIKey:        {},
	tokenAPIKeyCompact: {},
	"private_key":      {},
	"secret_key":       {},
	"authorization":    {},
	"auth_header":      {},
	"cookie":           {},
	"webhook_url":      {},
	"database_url":     {},
	"postgres_dsn":     {},
	"connection_url":   {},
}

func mightContainBearer(s string) bool {
	return containsFold(s, "bearer")
}

func mightContainQuerySecret(s string) bool {
	if !strings.ContainsAny(s, "?&;") || !strings.Contains(s, "=") {
		return false
	}
	for _, tok := range querySecretTokens {
		if containsFold(s, tok) {
			return true
		}
	}
	return false
}

// containsFold는 ASCII-only 입력에서만 고정 폭 윈도우로 substr를 fold-검색한다.
// (?i) 정규식·EqualFold는 ſ(U+017F)↔s, K(U+212A)↔k처럼 토큰과 바이트 폭이 다른
// 멀티바이트 룬을 fold-equivalent로 보므로, non-ASCII 바이트가 있으면 윈도우가
// 정렬되지 않아 superset 불변식이 깨진다. 이 경우 true를 반환해 정규식이 직접
// 판정하도록 위임한다(게이트는 정규식 매치의 superset이어야 한다).
func containsFold(s, substr string) bool {
	if len(substr) > len(s) {
		// 짧은 입력이라도 non-ASCII가 있으면 게이트를 통과시켜 정규식에 위임한다.
		return hasNonASCII(s)
	}
	for i := range len(s) {
		if s[i] >= 0x80 {
			return true
		}
		if i+len(substr) <= len(s) && strings.EqualFold(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func hasNonASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

func redactSecrets(s string) string {
	if mightContainBearer(s) {
		s = bearerTokenRegex.ReplaceAllString(s, "${1}***REDACTED***")
	}
	if mightContainQuerySecret(s) {
		s = querySecretRegex.ReplaceAllString(s, "${1}***REDACTED***")
	}
	return s
}

func (h *sanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *sanitizeHandler) Handle(ctx context.Context, record slog.Record) error {
	msg := redactSecrets(record.Message)
	changed := msg != record.Message

	if !changed {
		record.Attrs(func(attr slog.Attr) bool {
			if _, attrChanged := sanitizeAttrChanged(attr); attrChanged {
				changed = true
				return false
			}
			return true
		})
	}

	if !changed {
		return h.inner.Handle(ctx, record)
	}

	newRecord := slog.NewRecord(record.Time, record.Level, msg, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		newRecord.AddAttrs(sanitizeAttr(attr))
		return true
	})
	return h.inner.Handle(ctx, newRecord)
}

func (h *sanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, sanitizeAttr(attr))
	}
	return &sanitizeHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *sanitizeHandler) WithGroup(name string) slog.Handler {
	return &sanitizeHandler{inner: h.inner.WithGroup(name)}
}

func sanitizeAttr(attr slog.Attr) slog.Attr {
	out, _ := sanitizeAttrChanged(attr)
	return out
}

// sanitizeAttrChanged는 sanitizeAttr와 동일한 정규화를 수행하되, 결과가 원본 attr과
// byte-identical하게 같은지(changed=false) 여부를 함께 반환한다. Handle의 fast-path는
// 이 신호로 변경이 전혀 없을 때 record 재구축을 건너뛴다.
// Resolve 변경 판정에 Value.Equal을 쓰면 안 된다: KindAny끼리의 Equal은 내부 any를
// ==로 비교해 []int 같은 uncomparable 타입에서 panic한다. Resolve가 값을 바꾸는 건
// LogValuer뿐이므로 Kind 검사로 보수적으로(LogValuer면 항상 changed) 판정한다.
func sanitizeAttrChanged(attr slog.Attr) (slog.Attr, bool) {
	changed := attr.Value.Kind() == slog.KindLogValuer
	attr.Value = attr.Value.Resolve()

	if attr.Value.Kind() == slog.KindGroup {
		groupAttrs := attr.Value.Group()
		sanitized := make([]any, 0, len(groupAttrs))
		for _, groupAttr := range groupAttrs {
			out, c := sanitizeAttrChanged(groupAttr)
			if c {
				changed = true
			}
			sanitized = append(sanitized, out)
		}
		return slog.Group(attr.Key, sanitized...), changed
	}

	if attr.Value.Kind() != slog.KindString {
		return attr, changed
	}

	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, "***REDACTED***"), true
	}

	if isBroadValueKey(attr.Key) && isSecretLikeValue(attr.Value.String()) {
		return slog.String(attr.Key, "***REDACTED***"), true
	}

	redacted := redactSecrets(attr.Value.String())
	if redacted != attr.Value.String() {
		changed = true
	}
	return slog.String(attr.Key, redacted), changed
}

func isSensitiveKey(key string) bool {
	normalized := normalizeSensitiveKey(key)
	if normalized == "" {
		return false
	}

	if _, ok := sensitiveExactKeys[normalized]; ok {
		return true
	}

	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password") ||
		strings.HasSuffix(normalized, "_pwd") ||
		strings.HasSuffix(normalized, "_passwd") ||
		strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_private_key") ||
		strings.HasSuffix(normalized, "_secret_key")
}

func normalizeSensitiveKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}

var broadValueKeys = map[string]struct{}{
	"key": {},
}

func isBroadValueKey(key string) bool {
	_, ok := broadValueKeys[normalizeSensitiveKey(key)]
	return ok
}

var secretLikePrefixes = []string{
	"sk_", "pk_live", "pk_test", "rk_live", "rk_test",
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"xoxb-", "xoxp-", "xoxa-", "xoxr-",
	"akia", "asia",
	"aiza",
	"eyj",
}

const secretLikeMinLen = 24

func isSecretLikeValue(v string) bool {
	if hasSecretLikePrefix(v) {
		return true
	}
	return isHighEntropyToken(v)
}

func hasSecretLikePrefix(v string) bool {
	lower := strings.ToLower(v)
	for _, p := range secretLikePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func isHighEntropyToken(v string) bool {
	if len(v) < secretLikeMinLen {
		return false
	}
	var hasLower, hasUpper, hasDigit bool
	for i := range len(v) {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '_' || c == '-' || c == '.' || c == '+' || c == '/' || c == '=':
		default:
			return false
		}
	}
	return hasLower && hasUpper && hasDigit
}
