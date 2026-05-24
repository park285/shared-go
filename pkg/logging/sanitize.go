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
	querySecretRegex = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|token|password|client_secret|secret)=)[^&\s]+`)
)

func (h *SanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SanitizeHandler) Handle(ctx context.Context, record slog.Record) error {
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
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

	value := attr.Value.String()
	value = bearerTokenRegex.ReplaceAllString(value, "${1}***REDACTED***")
	value = querySecretRegex.ReplaceAllString(value, "${1}***REDACTED***")
	return slog.String(attr.Key, value)
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
		"secret":         true,
		"key":            true,
		"client_secret":  true,
		"api_key":        true,
		"apikey":         true,
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
		strings.HasSuffix(normalized, "_api_key")
}

func normalizeSensitiveKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return key
}
