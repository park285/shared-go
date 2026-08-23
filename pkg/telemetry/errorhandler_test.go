package telemetry

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInstallGlobalProvider_SetsErrorHandler(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	prevHandler := otel.GetErrorHandler()
	sentinelTP := sdktrace.NewTracerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		otel.SetErrorHandler(prevHandler)
		_ = sentinelTP.Shutdown(t.Context())
	})

	var buf bytes.Buffer
	oldLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	installGlobalProvider(sentinelTP)

	otel.GetErrorHandler().Handle(errors.New("collector unreachable"))

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected error handler to emit a log record, got none")
	}
	rec := map[string]any{}
	if err := jsonv2.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("unmarshal log record: %v", err)
	}
	if rec["level"] != "WARN" {
		t.Fatalf("expected WARN level, got %v", rec["level"])
	}
	if !strings.Contains(out, "collector unreachable") {
		t.Fatalf("expected error message in log, got %q", out)
	}
}
