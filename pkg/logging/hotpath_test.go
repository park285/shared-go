package logging

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

type hotpathCaptureHandler struct {
	enabledCalls int
	handled      int
	record       slog.Record
}

func (h *hotpathCaptureHandler) Enabled(context.Context, slog.Level) bool {
	h.enabledCalls++
	return true
}

func (h *hotpathCaptureHandler) Handle(_ context.Context, record slog.Record) error {
	h.handled++
	h.record = record.Clone()
	return nil
}

func (h *hotpathCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *hotpathCaptureHandler) WithGroup(string) slog.Handler      { return h }

type hotpathDiscardHandler struct{}

func (hotpathDiscardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (hotpathDiscardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h hotpathDiscardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h hotpathDiscardHandler) WithGroup(string) slog.Handler           { return h }

func TestLog_CommonPathZeroAlloc(t *testing.T) {
	logger := slog.New(hotpathDiscardHandler{})
	ctx := WithRequestID(WithRuntime(context.Background(), "bot"), "req-1")

	got := testing.AllocsPerRun(1000, func() {
		Log(ctx, logger, slog.LevelInfo, "request.completed", "request completed",
			slog.String("method", "GET"),
			slog.Int("status", 200),
		)
	})
	if got != 0 {
		t.Fatalf("Log common-path allocs = %v, want 0", got)
	}
}

func TestLog_CallsEnabledOnce(t *testing.T) {
	handler := &hotpathCaptureHandler{}
	logger := slog.New(handler)

	Info(context.Background(), logger, "event", "message")

	if handler.enabledCalls != 1 {
		t.Fatalf("Enabled calls = %d, want 1", handler.enabledCalls)
	}
	if handler.handled != 1 {
		t.Fatalf("Handle calls = %d, want 1", handler.handled)
	}
}

func TestLog_CapturesWrapperCaller(t *testing.T) {
	handler := &hotpathCaptureHandler{}
	logger := slog.New(handler)

	Info(context.Background(), logger, "event", "message")

	frames := runtime.CallersFrames([]uintptr{handler.record.PC})
	frame, _ := frames.Next()
	if !strings.HasSuffix(frame.Function, ".TestLog_CapturesWrapperCaller") {
		t.Fatalf("source function = %q, want test caller", frame.Function)
	}
	if !strings.HasSuffix(frame.File, "pkg/logging/hotpath_test.go") {
		t.Fatalf("source file = %q, want hotpath_test.go", frame.File)
	}
}

func TestContextAttrs_EmptyZeroAlloc(t *testing.T) {
	got := testing.AllocsPerRun(1000, func() {
		if attrs := ContextAttrs(context.Background()); attrs != nil {
			panic("empty context attrs must be nil")
		}
	})
	if got != 0 {
		t.Fatalf("ContextAttrs(empty) allocs = %v, want 0", got)
	}
}

func TestShortenSource_ReusesSourceValue(t *testing.T) {
	source := &slog.Source{
		Function: "main.run",
		File:     "/build/root/pkg/logging/file.go",
		Line:     42,
	}
	attr := slog.Any(slog.SourceKey, source)

	out := shortenSource(nil, attr)
	if out.Value.Any() != source {
		t.Fatal("shortenSource replaced the source object")
	}
	if source.File != "logging/file.go" {
		t.Fatalf("source file = %q, want %q", source.File, "logging/file.go")
	}
}

func TestSanitizeCleanGroup_ZeroAlloc(t *testing.T) {
	attr := slog.Group("request",
		slog.String("method", "GET"),
		slog.String("path", "/api/users"),
		slog.Int("status", 200),
	)
	var (
		out     slog.Attr
		changed bool
	)

	got := testing.AllocsPerRun(1000, func() {
		out, changed = sanitizeAttrChanged(attr)
	})
	if got != 0 {
		t.Fatalf("clean group sanitize allocs = %v, want 0", got)
	}
	if changed {
		t.Fatal("clean group reported a change")
	}
	if !out.Equal(attr) {
		t.Fatalf("clean group changed: got %v, want %v", out, attr)
	}
}

func TestSanitizeGroupCopyOnWrite_MasksNestedValue(t *testing.T) {
	attr := slog.Group("request",
		slog.String("method", "GET"),
		slog.Group("headers",
			slog.String("authorization", "Bearer secret"),
			slog.String("accept", "application/json"),
		),
		slog.Int("status", 200),
	)

	out, changed := sanitizeAttrChanged(attr)
	if !changed {
		t.Fatal("sensitive nested group reported no change")
	}

	requestAttrs := out.Value.Group()
	if len(requestAttrs) != 3 {
		t.Fatalf("request attrs = %d, want 3", len(requestAttrs))
	}
	headers := requestAttrs[1].Value.Group()
	if len(headers) != 2 {
		t.Fatalf("header attrs = %d, want 2", len(headers))
	}
	if got := headers[0].Value.String(); got != redactedValue {
		t.Fatalf("authorization = %q, want %q", got, redactedValue)
	}
	if got := headers[1].Value.String(); got != "application/json" {
		t.Fatalf("accept = %q, want %q", got, "application/json")
	}

	originalHeaders := attr.Value.Group()[1].Value.Group()
	if got := originalHeaders[0].Value.String(); got != "Bearer secret" {
		t.Fatalf("caller-owned group mutated: authorization = %q", got)
	}
}

func BenchmarkLogCommonPath(b *testing.B) {
	logger := slog.New(hotpathDiscardHandler{})
	ctx := WithRequestID(WithRuntime(context.Background(), "bot"), "req-1")
	b.ReportAllocs()
	for range b.N {
		Log(ctx, logger, slog.LevelInfo, "request.completed", "request completed",
			slog.String("method", "GET"),
			slog.Int("status", 200),
		)
	}
}

func BenchmarkSanitizeCleanGroup(b *testing.B) {
	attr := slog.Group("request",
		slog.String("method", "GET"),
		slog.String("path", "/api/users"),
		slog.Int("status", 200),
	)
	var out slog.Attr

	b.ReportAllocs()
	for range b.N {
		out, _ = sanitizeAttrChanged(attr)
	}
	if !out.Equal(attr) {
		b.Fatal("clean group changed")
	}
}
