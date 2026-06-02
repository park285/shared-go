package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// recordSink는 Handle로 전달된 최종 slog.Record를 캡처한다.
type recordSink struct {
	records []slog.Record
}

func (s *recordSink) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (s *recordSink) Handle(_ context.Context, r slog.Record) error {
	s.records = append(s.records, r.Clone())
	return nil
}
func (s *recordSink) WithAttrs(_ []slog.Attr) slog.Handler { return s }
func (s *recordSink) WithGroup(_ string) slog.Handler      { return s }

// TestSanitizeHandler_MessageMasking — Behavior 1 (RED expected)
// Message 문자열 안의 Bearer 토큰과 query secret이 마스킹돼야 한다.
func TestSanitizeHandler_MessageMasking(t *testing.T) {
	sink := &recordSink{}
	h := NewSanitizeHandler(sink)
	logger := slog.New(h)

	logger.Info("auth: Bearer abc123def and ?token=secret123 received")

	if len(sink.records) == 0 {
		t.Fatal("no records captured by sink")
	}
	msg := sink.records[0].Message

	if strings.Contains(msg, "abc123def") {
		t.Errorf("Bearer token value must be masked in message, got message: %q", msg)
	}
	if strings.Contains(msg, "secret123") {
		t.Errorf("query token value must be masked in message, got message: %q", msg)
	}
	if !strings.Contains(msg, "***REDACTED***") {
		t.Errorf("expected ***REDACTED*** in message, got: %q", msg)
	}
}

// TextHandler 경유로도 출력에 마스킹이 적용되는지 통합 검증.
func TestSanitizeHandler_MessageMasking_TextOutput(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := NewSanitizeHandler(base)
	slog.New(h).Info("auth: Bearer abc123def and ?token=secret123 received")

	out := buf.String()
	if strings.Contains(out, "abc123def") {
		t.Errorf("Bearer token value must be masked in text output, got: %s", out)
	}
	if strings.Contains(out, "secret123") {
		t.Errorf("query token value must be masked in text output, got: %s", out)
	}
}

// TestIsSensitiveKey_BareKeyNotMasked — Behavior 3 (RED expected)
// "key" 단독 키는 마스킹하지 않아야 한다.
func TestIsSensitiveKey_BareKeyNotMasked(t *testing.T) {
	if isSensitiveKey("key") {
		t.Errorf(`isSensitiveKey("key") = true, want false: bare "key" should not be masked`)
	}
}

// TestIsSensitiveKey_ApiKeyVariantsMasked — Behavior 3 (기존 동작 보존)
// api_key / apikey / x_api_key 변형은 여전히 마스킹돼야 한다.
func TestIsSensitiveKey_ApiKeyVariantsMasked(t *testing.T) {
	cases := []string{"api_key", "apikey", "x_api_key", "MY_API_KEY"}
	for _, k := range cases {
		if !isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = false, want true", k)
		}
	}
}
