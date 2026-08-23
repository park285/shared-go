package logging

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

type operationTestError struct{}

func (operationTestError) Error() string {
	return "operation failed for test"
}

func (operationTestError) Code() string {
	return "test_code"
}

func (operationTestError) Retryable() bool {
	return true
}

func TestRunOperationLogsSuccessLifecycle(t *testing.T) {
	var buf bytes.Buffer
	logger := operationTestLogger(&buf)

	var gotCtx context.Context
	err := RunOperation(context.Background(), logger, OperationOptions{
		Name:      "sync",
		IDPrefix:  "sync-job",
		Runtime:   "worker",
		Component: "logging",
		Attrs:     []slog.Attr{slog.String("extra", "value")},
	}, func(ctx context.Context) error {
		gotCtx = ctx
		return nil
	})
	if err != nil {
		t.Fatalf("RunOperation returned error: %v", err)
	}
	if gotCtx == nil {
		t.Fatal("operation function received nil context")
	}
	if got := jobIDFromContext(gotCtx); !strings.HasPrefix(got, "sync_job_") {
		t.Fatalf("jobIDFromContext() = %q, want sync_job_ prefix", got)
	}
	if got := runtimeFromContext(gotCtx); got != "worker" {
		t.Fatalf("runtimeFromContext() = %q, want %q", got, "worker")
	}
	if got := componentFromContext(gotCtx); got != "logging" {
		t.Fatalf("componentFromContext() = %q, want %q", got, "logging")
	}

	records := operationLogRecords(t, &buf)
	if len(records) != 2 {
		t.Fatalf("got %d log records, want 2: %#v", len(records), records)
	}
	requireRecordValue(t, records[0], "event", "sync.started")
	requireRecordValue(t, records[0], "msg", "operation started")
	requireRecordValue(t, records[0], "operation", "sync")
	requireRecordValue(t, records[0], "runtime", "worker")
	requireRecordValue(t, records[0], "component", "logging")
	requireRecordValue(t, records[0], "extra", "value")

	requireRecordValue(t, records[1], "event", "sync.succeeded")
	requireRecordValue(t, records[1], "msg", "operation succeeded")
	requireRecordValue(t, records[1], "operation", "sync")
	requireNumericRecordValue(t, records[1], "duration_ms")
}

func TestRunOperationLogsFailureWithErrorAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := operationTestLogger(&buf)
	wantErr := operationTestError{}

	err := RunOperation(context.Background(), logger, OperationOptions{Name: "sync"}, func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunOperation error = %v, want %v", err, wantErr)
	}

	records := operationLogRecords(t, &buf)
	if len(records) != 2 {
		t.Fatalf("got %d log records, want 2: %#v", len(records), records)
	}
	failure := records[1]
	requireRecordValue(t, failure, "level", "ERROR")
	requireRecordValue(t, failure, "event", "sync.failed")
	requireRecordValue(t, failure, "msg", "operation failed")
	requireRecordValue(t, failure, "operation", "sync")
	requireNumericRecordValue(t, failure, "duration_ms")
	requireRecordValue(t, failure, "error_type", "operationTestError")
	requireRecordValue(t, failure, "error_message", "operation failed for test")
	requireRecordValue(t, failure, "error_code", "test_code")
	requireRecordValue(t, failure, "retryable", true)
}

func TestRunOperationSkipsStartLog(t *testing.T) {
	var buf bytes.Buffer
	logger := operationTestLogger(&buf)

	err := RunOperation(context.Background(), logger, OperationOptions{
		Name:         "sync",
		SkipStartLog: true,
	}, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("RunOperation returned error: %v", err)
	}

	records := operationLogRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1: %#v", len(records), records)
	}
	requireRecordValue(t, records[0], "event", "sync.succeeded")
}

func TestRunOperationUsesCustomEvents(t *testing.T) {
	tests := []struct {
		name       string
		fn         func(context.Context) error
		wantErr    bool
		wantEvents []string
	}{
		{
			name: "success",
			fn: func(context.Context) error {
				return nil
			},
			wantEvents: []string{"custom.start", "custom.success"},
		},
		{
			name: "failure",
			fn: func(context.Context) error {
				return errors.New("fail")
			},
			wantErr:    true,
			wantEvents: []string{"custom.start", "custom.failure"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := operationTestLogger(&buf)

			err := RunOperation(context.Background(), logger, OperationOptions{
				Name:         "sync",
				StartEvent:   "custom.start",
				SuccessEvent: "custom.success",
				FailureEvent: "custom.failure",
			}, tt.fn)
			if tt.wantErr && err == nil {
				t.Fatal("RunOperation returned nil error, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("RunOperation returned error: %v", err)
			}

			records := operationLogRecords(t, &buf)
			if len(records) != len(tt.wantEvents) {
				t.Fatalf("got %d log records, want %d: %#v", len(records), len(tt.wantEvents), records)
			}
			for i, wantEvent := range tt.wantEvents {
				requireRecordValue(t, records[i], "event", wantEvent)
			}
		})
	}
}

type operationSourceHandler struct {
	pcs []uintptr
}

func (h *operationSourceHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *operationSourceHandler) Handle(_ context.Context, record slog.Record) error {
	h.pcs = append(h.pcs, record.PC)
	return nil
}

func (h *operationSourceHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *operationSourceHandler) WithGroup(string) slog.Handler      { return h }

func TestRunOperationCapturesCallerSource(t *testing.T) {
	tests := []struct {
		name string
		fn   func(context.Context) error
	}{
		{name: "success", fn: func(context.Context) error { return nil }},
		{name: "failure", fn: func(context.Context) error { return errors.New("fail") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &operationSourceHandler{}

			_ = RunOperation(context.Background(), slog.New(handler), OperationOptions{Name: "sync"}, tt.fn)

			if len(handler.pcs) != 2 {
				t.Fatalf("got %d records, want 2 (started + terminal)", len(handler.pcs))
			}
			for i, pc := range handler.pcs {
				frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
				if !strings.HasSuffix(frame.File, "pkg/logging/operation_test.go") {
					t.Fatalf("record %d source = %s:%d, want the RunOperation call site rather than the RunOperation body", i, frame.File, frame.Line)
				}
				if !strings.Contains(frame.Function, "TestRunOperationCapturesCallerSource") {
					t.Fatalf("record %d source function = %q, want the RunOperation caller", i, frame.Function)
				}
			}
		})
	}
}

func TestOperationContextAddsFallbackJobIDAndRuntimeFromNilContext(t *testing.T) {
	var nilCtx context.Context
	ctx := operationContext(nilCtx, OperationOptions{
		Name:      "sync",
		IDPrefix:  "batch",
		Runtime:   "worker",
		Component: "logging",
	})

	if ctx == nil {
		t.Fatal("operationContext returned nil context")
	}
	if got := jobIDFromContext(ctx); !strings.HasPrefix(got, "batch_") {
		t.Fatalf("jobIDFromContext() = %q, want batch_ prefix", got)
	}
	if got := runtimeFromContext(ctx); got != "worker" {
		t.Fatalf("runtimeFromContext() = %q, want %q", got, "worker")
	}
	if got := componentFromContext(ctx); got != "logging" {
		t.Fatalf("componentFromContext() = %q, want %q", got, "logging")
	}
}

func TestOperationContextWithJobIDPreservesExistingAndCreatesNew(t *testing.T) {
	t.Run("preserves existing JobID", func(t *testing.T) {
		ctx := WithJobID(context.Background(), "existing-job")
		gotCtx := operationContextWithJobID(ctx, OperationOptions{IDPrefix: "new-job"})

		if got := jobIDFromContext(gotCtx); got != "existing-job" {
			t.Fatalf("jobIDFromContext() = %q, want %q", got, "existing-job")
		}
	})

	t.Run("creates new JobID", func(t *testing.T) {
		gotCtx := operationContextWithJobID(context.Background(), OperationOptions{IDPrefix: "new-job"})

		if got := jobIDFromContext(gotCtx); !strings.HasPrefix(got, "new_job_") {
			t.Fatalf("jobIDFromContext() = %q, want new_job_ prefix", got)
		}
	})
}

func TestOperationContextWithJobIDFallsBackToOperationName(t *testing.T) {
	ctx := operationContextWithJobID(context.Background(), OperationOptions{
		Name:     "Video.Job",
		IDPrefix: "   ",
	})

	if got := jobIDFromContext(ctx); !strings.HasPrefix(got, "video_job_") {
		t.Fatalf("jobIDFromContext() = %q, want video_job_ prefix", got)
	}
}

func TestOperationContextWithRuntimeIgnoresEmptyRuntimeAndComponent(t *testing.T) {
	ctx := WithRuntime(context.Background(), "runtime-1")
	ctx = WithComponent(ctx, "component-1")

	gotCtx := operationContextWithRuntime(ctx, OperationOptions{
		Runtime:   "",
		Component: "",
	})

	if got := runtimeFromContext(gotCtx); got != "runtime-1" {
		t.Fatalf("runtimeFromContext() = %q, want %q", got, "runtime-1")
	}
	if got := componentFromContext(gotCtx); got != "component-1" {
		t.Fatalf("componentFromContext() = %q, want %q", got, "component-1")
	}
}

func TestOperationName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "trims name", in: "  sync  ", want: "sync"},
		{name: "empty falls back", in: "", want: "operation"},
		{name: "blank falls back", in: " \t\n ", want: "operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := operationName(tt.in); got != tt.want {
				t.Fatalf("operationName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOperationAttrs(t *testing.T) {
	attrs := operationAttrs("sync", []slog.Attr{
		slog.String("extra", "value"),
		slog.Int("count", 2),
	})

	if len(attrs) != 3 {
		t.Fatalf("len(operationAttrs()) = %d, want 3", len(attrs))
	}
	if attrs[0].Key != "operation" || attrs[0].Value.String() != "sync" {
		t.Fatalf("attrs[0] = %#v, want operation=sync", attrs[0])
	}
	if attrs[1].Key != "extra" || attrs[1].Value.String() != "value" {
		t.Fatalf("attrs[1] = %#v, want extra=value", attrs[1])
	}
	if attrs[2].Key != "count" || attrs[2].Value.Int64() != 2 {
		t.Fatalf("attrs[2] = %#v, want count=2", attrs[2])
	}
}

func TestEventOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "value", value: "custom", fallback: "fallback", want: "custom"},
		{name: "empty", value: "", fallback: "fallback", want: "fallback"},
		{name: "blank", value: " \t\n ", fallback: "fallback", want: "fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventOrDefault(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("eventOrDefault(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func operationTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func operationLogRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	output := strings.TrimSpace(buf.String())
	if output == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := jsonv2.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal log record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func requireRecordValue(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()

	got, ok := record[key]
	if !ok {
		t.Fatalf("record missing %q: %#v", key, record)
	}
	if got != want {
		t.Fatalf("record[%q] = %#v, want %#v", key, got, want)
	}
}

func requireNumericRecordValue(t *testing.T, record map[string]any, key string) {
	t.Helper()

	got, ok := record[key]
	if !ok {
		t.Fatalf("record missing %q: %#v", key, record)
	}
	value, ok := got.(float64)
	if !ok {
		t.Fatalf("record[%q] = %#v, want number", key, got)
	}
	if value < 0 {
		t.Fatalf("record[%q] = %v, want non-negative number", key, value)
	}
}
