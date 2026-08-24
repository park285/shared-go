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
	ctx := WithRequestID(WithRuntime(t.Context(), "bot"), "req-1")

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

	Info(t.Context(), logger, "event", "message")

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

	Info(t.Context(), logger, "event", "message")

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
		if attrs := ContextAttrs(t.Context()); attrs != nil {
			panic("empty context attrs must be nil")
		}
	})
	if got != 0 {
		t.Fatalf("ContextAttrs(empty) allocs = %v, want 0", got)
	}
}

func TestBroadValueKeyNormalizesRawKeys(t *testing.T) {
	if !isBroadValueKey(" KEY ") {
		t.Fatal("raw broad-value key was not normalized")
	}

	if isBroadValueKey("api_key") {
		t.Fatal("sensitive exact key must not be classified as a broad-value key")
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

func TestErrorHelpers_CaptureCallerSource(t *testing.T) {
	cases := map[string]func(context.Context, *slog.Logger){
		"LogAndWrapError": func(ctx context.Context, logger *slog.Logger) {
			if err := LogAndWrapError(ctx, logger, "op", context.DeadlineExceeded); err == nil {
				t.Fatal("LogAndWrapError() = nil, want error")
			}
		},
		"LogWarnWithErrorAttrs": func(ctx context.Context, logger *slog.Logger) {
			LogWarnWithErrorAttrs(ctx, logger, "event", "message", context.DeadlineExceeded)
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			handler := &hotpathCaptureHandler{}
			call(t.Context(), slog.New(handler))

			frames := runtime.CallersFrames([]uintptr{handler.record.PC})
			frame, _ := frames.Next()

			if !strings.HasSuffix(frame.File, "pkg/logging/hotpath_test.go") {
				t.Fatalf("source file = %q, want the call site rather than the helper body", frame.File)
			}
		})
	}
}

func BenchmarkLogCommonPath(b *testing.B) {
	logger := slog.New(hotpathDiscardHandler{})
	ctx := WithRequestID(WithRuntime(b.Context(), "bot"), "req-1")

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
