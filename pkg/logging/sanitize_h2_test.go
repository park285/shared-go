package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

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

func TestSanitizeHandler_MessageMasking(t *testing.T) {
	sink := &recordSink{}
	h := newSanitizeHandler(sink)
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

func TestSanitizeHandler_MessageMasking_TextOutput(t *testing.T) {
	var buf bytes.Buffer

	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newSanitizeHandler(base)
	slog.New(h).Info("auth: Bearer abc123def and ?token=secret123 received")

	out := buf.String()
	if strings.Contains(out, "abc123def") {
		t.Errorf("Bearer token value must be masked in text output, got: %s", out)
	}

	if strings.Contains(out, "secret123") {
		t.Errorf("query token value must be masked in text output, got: %s", out)
	}
}

func TestBareKey_NameAloneNotMasked_ValueGatesRedaction_8e92058d(t *testing.T) {
	if isSensitiveKey("key") {
		t.Error(`isSensitiveKey("key") = true, want false: key name alone must not force redaction`)
	}

	out := sanitizeValue(t, "key", "sk_live_"+"FAKEvalueNotARealStripeKey")
	if strings.Contains(out, "sk_live_") || !strings.Contains(out, "***REDACTED***") {
		t.Errorf("secret-like literal key field value must be masked, got: %s", out)
	}
}

func TestIsSensitiveKey_ApiKeyVariantsMasked(t *testing.T) {
	cases := []string{"api_key", "apikey", "x_api_key", "MY_API_KEY"}
	for _, k := range cases {
		if !isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = false, want true", k)
		}
	}
}
