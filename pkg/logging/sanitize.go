package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

type SanitizeHandler struct {
	inner slog.Handler
}

func NewSanitizeHandler(inner slog.Handler) *SanitizeHandler {
	return &SanitizeHandler{inner: inner}
}

// 민감 키 리스트 (case-insensitive 매칭)
var (
	bearerTokenRegex = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	querySecretRegex = regexp.MustCompile(`(?i)([?&;](?:key|api_key|apikey|token|password|pwd|passwd|client_secret|secret|private_key|secret_key)=)[^&\s]+`)
)

// querySecretTokens는 querySecretRegex가 매치할 수 있는 키 이름 집합으로,
// 정규식 실행 전 싼 substring pre-check 게이트에 쓰인다. 정규식이 매치하는
// 입력은 반드시 이 토큰 중 하나를 case-insensitive로 포함하므로 게이트는 안전하다.
var querySecretTokens = []string{
	"key", "api_key", "apikey", "token", "password", "pwd", "passwd",
	"client_secret", "secret", "private_key", "secret_key",
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

func containsFold(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(substr)], substr) {
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

func (h *SanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SanitizeHandler) Handle(ctx context.Context, record slog.Record) error {
	msg := redactSecrets(record.Message)
	newRecord := slog.NewRecord(record.Time, record.Level, msg, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		newRecord.AddAttrs(sanitizeAttr(attr))
		return true
	})
	return h.inner.Handle(ctx, newRecord)
}

func (h *SanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, sanitizeAttr(attr))
	}
	return &SanitizeHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *SanitizeHandler) WithGroup(name string) slog.Handler {
	return &SanitizeHandler{inner: h.inner.WithGroup(name)}
}

func sanitizeAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()

	if attr.Value.Kind() == slog.KindGroup {
		groupAttrs := attr.Value.Group()
		sanitized := make([]any, 0, len(groupAttrs))
		for _, groupAttr := range groupAttrs {
			sanitized = append(sanitized, sanitizeAttr(groupAttr))
		}
		return slog.Group(attr.Key, sanitized...)
	}

	if attr.Value.Kind() != slog.KindString {
		return attr
	}

	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, "***REDACTED***")
	}

	return slog.String(attr.Key, redactSecrets(attr.Value.String()))
}

func isSensitiveKey(key string) bool {
	normalized := normalizeSensitiveKey(key)
	if normalized == "" {
		return false
	}

	exact := map[string]bool{
		"token":          true,
		"bot_token":      true,
		"access_token":   true,
		"refresh_token":  true,
		"password":       true,
		"pwd":            true,
		"passwd":         true,
		"secret":         true,
		"client_secret":  true,
		"api_key":        true,
		"apikey":         true,
		"private_key":    true,
		"secret_key":     true,
		"authorization":  true,
		"auth_header":    true,
		"cookie":         true,
		"webhook_url":    true,
		"database_url":   true,
		"postgres_dsn":   true,
		"connection_url": true,
	}
	if exact[normalized] {
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
