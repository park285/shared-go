package logging

import (
	"context"
	"log/slog"
	"testing"
)

type capturedLogRecord struct {
	ctx   context.Context
	level slog.Level
	attrs map[string]slog.Value
}

type enabledCall struct {
	ctx   context.Context
	level slog.Level
}

type captureLogHandler struct {
	enabled      bool
	enabledCalls []enabledCall
	records      []capturedLogRecord
}

func newCaptureLogHandler(enabled bool) *captureLogHandler {
	return &captureLogHandler{enabled: enabled}
}

func (h *captureLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.enabledCalls = append(h.enabledCalls, enabledCall{ctx: ctx, level: level})
	return h.enabled
}

func (h *captureLogHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := make(map[string]slog.Value)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	h.records = append(h.records, capturedLogRecord{
		ctx:   ctx,
		level: record.Level,
		attrs: attrs,
	})
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureLogHandler) WithGroup(string) slog.Handler {
	return h
}

func TestLevelWrappersDelegateToLogLevel(t *testing.T) {
	tests := []struct {
		name string
		log  func(context.Context, *slog.Logger, string, string, ...slog.Attr)
		want slog.Level
	}{
		{name: "debug", log: Debug, want: slog.LevelDebug},
		{name: "info", log: Info, want: slog.LevelInfo},
		{name: "warn", log: Warn, want: slog.LevelWarn},
		{name: "error", log: Error, want: slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newCaptureLogHandler(true)
			logger := slog.New(handler)

			tt.log(context.Background(), logger, "event", "message")

			if len(handler.records) != 1 {
				t.Fatalf("got %d records, want 1", len(handler.records))
			}
			if got := handler.records[0].level; got != tt.want {
				t.Fatalf("record level = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogWithNilLoggerNoops(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Log panicked with nil logger: %v", r)
		}
	}()

	Log(context.Background(), nil, slog.LevelInfo, "event", "message")
}

func TestLogWithNilContextFallsBackToBackground(t *testing.T) {
	var nilCtx context.Context
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	Log(nilCtx, logger, slog.LevelInfo, "event", "message")

	if len(handler.enabledCalls) == 0 {
		t.Fatal("Enabled was not called")
	}
	for i, call := range handler.enabledCalls {
		if call.ctx == nil {
			t.Fatalf("Enabled call %d received nil context", i)
		}
	}
	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	if handler.records[0].ctx == nil {
		t.Fatal("Handle received nil context")
	}
}

func TestLogSkipsWhenLevelDisabled(t *testing.T) {
	handler := newCaptureLogHandler(false)
	logger := slog.New(handler)

	Log(context.Background(), logger, slog.LevelInfo, "event", "message")

	if len(handler.enabledCalls) != 1 {
		t.Fatalf("got %d Enabled calls, want 1", len(handler.enabledCalls))
	}
	if got := handler.enabledCalls[0].level; got != slog.LevelInfo {
		t.Fatalf("Enabled level = %v, want %v", got, slog.LevelInfo)
	}
	if len(handler.records) != 0 {
		t.Fatalf("got %d records, want 0", len(handler.records))
	}
}

func TestLogAddsEventAttrOnlyWhenPresent(t *testing.T) {
	tests := []struct {
		name      string
		event     string
		wantEvent string
	}{
		{name: "empty event", event: " \t\n ", wantEvent: ""},
		{name: "non-empty event", event: "sync.started", wantEvent: "sync.started"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newCaptureLogHandler(true)
			logger := slog.New(handler)

			Log(context.Background(), logger, slog.LevelInfo, tt.event, "message")

			if len(handler.records) != 1 {
				t.Fatalf("got %d records, want 1", len(handler.records))
			}
			got, ok := handler.records[0].attrs["event"]
			if tt.wantEvent == "" {
				if ok {
					t.Fatalf("event attr = %q, want omitted", got.String())
				}
				return
			}
			if !ok {
				t.Fatal("event attr missing")
			}
			if got.String() != tt.wantEvent {
				t.Fatalf("event attr = %q, want %q", got.String(), tt.wantEvent)
			}
		})
	}
}

func TestLogMergesContextAttrsAndAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	ctx := WithJobID(context.Background(), "job-1")

	Log(ctx, logger, slog.LevelInfo, "sync.started", "message", slog.String("extra", "value"))

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	requireCapturedAttr(t, record, "event", "sync.started")
	requireCapturedAttr(t, record, "job_id", "job-1")
	requireCapturedAttr(t, record, "extra", "value")
}

func TestLogMessageFallback(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		message string
		want    string
	}{
		{name: "message wins", event: "event", message: " message ", want: "message"},
		{name: "event fallback", event: " event ", message: " \t\n ", want: "event"},
		{name: "default fallback", event: "", message: "", want: "log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logMessage(tt.event, tt.message); got != tt.want {
				t.Fatalf("logMessage(%q, %q) = %q, want %q", tt.event, tt.message, got, tt.want)
			}
		})
	}
}

func requireCapturedAttr(t *testing.T, record capturedLogRecord, key string, want string) {
	t.Helper()

	got, ok := record.attrs[key]
	if !ok {
		t.Fatalf("record missing %q: %#v", key, record.attrs)
	}
	if got.String() != want {
		t.Fatalf("record[%q] = %q, want %q", key, got.String(), want)
	}
}
