package telemetry

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogErrorHandler_RedactsCredentialsInExporterError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		mustAbsent []string
		mustHave   string
	}{
		{
			name:       "credential in endpoint url",
			err:        errors.New("connect to https://otel:s3cr3t-p4ss@collector.internal:4317 failed"),
			mustAbsent: []string{"s3cr3t-p4ss"},
			mustHave:   "collector.internal",
		},
		{
			name:       "bearer token in header dump",
			err:        errors.New(`rpc error: metadata authorization="Bearer abc123.def456.ghi789"`),
			mustAbsent: []string{"abc123.def456.ghi789"},
			mustHave:   "rpc error",
		},
		{
			name:       "query secret in endpoint",
			err:        errors.New("export failed: https://collector:4318/v1/traces?api_key=super-secret-value"),
			mustAbsent: []string{"super-secret-value"},
			mustHave:   "/v1/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			old := slog.Default()

			t.Cleanup(func() { slog.SetDefault(old) })
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

			slogErrorHandler{}.Handle(tt.err)

			out := buf.String()
			if out == "" {
				t.Fatal("expected a log record, got none")
			}

			for _, secret := range tt.mustAbsent {
				if strings.Contains(out, secret) {
					t.Fatalf("credential %q leaked into log: %s", secret, out)
				}
			}

			if !strings.Contains(out, tt.mustHave) {
				t.Fatalf("redaction removed diagnostic context %q: %s", tt.mustHave, out)
			}

			if !strings.Contains(out, "REDACTED") {
				t.Fatalf("expected a redaction marker in log: %s", out)
			}
		})
	}
}

func TestSlogErrorHandler_NilErrorEmitsNothing(t *testing.T) {
	var buf bytes.Buffer

	old := slog.Default()

	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	slogErrorHandler{}.Handle(nil)

	if buf.Len() != 0 {
		t.Fatalf("nil error wrote %q, want nothing", buf.String())
	}
}
