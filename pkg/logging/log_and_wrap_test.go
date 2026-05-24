package logging

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type logAndWrapTypedError struct {
	message string
}

func (e *logAndWrapTypedError) Error() string {
	return e.message
}

type logAndWrapCodedRetryableError struct {
	message   string
	code      string
	retryable bool
}

func (e *logAndWrapCodedRetryableError) Error() string {
	return e.message
}

func (e *logAndWrapCodedRetryableError) Code() string {
	return e.code
}

func (e *logAndWrapCodedRetryableError) Retryable() bool {
	return e.retryable
}

func TestLogAndWrapError_NilErrorReturnsNil(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	err := LogAndWrapError(context.Background(), logger, "sync.poll", nil)

	if err != nil {
		t.Fatalf("LogAndWrapError returned %v, want nil", err)
	}
	if len(handler.enabledCalls) != 0 {
		t.Fatalf("got %d Enabled calls, want 0", len(handler.enabledCalls))
	}
	if len(handler.records) != 0 {
		t.Fatalf("got %d records, want 0", len(handler.records))
	}
}

func TestLogAndWrapError_WrapsErrorWithOpPrefix(t *testing.T) {
	cause := &logAndWrapTypedError{message: "typed failure"}

	err := LogAndWrapError(context.Background(), nil, "sync.poll", cause)

	if err == nil {
		t.Fatal("LogAndWrapError returned nil, want wrapped error")
	}
	if got, want := err.Error(), "sync.poll: typed failure"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped error does not match cause with errors.Is")
	}
	var target *logAndWrapTypedError
	if !errors.As(err, &target) {
		t.Fatal("wrapped error does not match cause type with errors.As")
	}
}

func TestLogAndWrapError_LogsFailureEvent(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogAndWrapError(context.Background(), logger, "sync.poll", errors.New("boom"))

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	requireCapturedAttr(t, handler.records[0], "event", "sync.poll.failed")
}

func TestLogAndWrapError_IncludesErrorAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	cause := &logAndWrapTypedError{message: "typed failure"}

	LogAndWrapError(context.Background(), logger, "sync.poll", cause)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	requireCapturedAttr(t, handler.records[0], "error_type", "logAndWrapTypedError")
	requireCapturedAttr(t, handler.records[0], "error_message", "typed failure")
}

func TestLogAndWrap_AttrMergeCallerOverridesErrorAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogAndWrapError(
		context.Background(),
		logger,
		"sync.poll",
		errors.New("boom"),
		slog.String("error_message", "custom"),
	)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	requireCapturedAttr(t, handler.records[0], "error_message", "custom")
}

func TestLogAndWrap_IncludesErrorCodeAndRetryable(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	cause := &logAndWrapCodedRetryableError{
		message:   "temporary failure",
		code:      "TEMPORARY_FAILURE",
		retryable: true,
	}

	LogAndWrapError(context.Background(), logger, "sync.poll", cause)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	requireCapturedAttr(t, record, "error_code", "TEMPORARY_FAILURE")

	got, ok := record.attrs["retryable"]
	if !ok {
		t.Fatalf("record missing %q: %#v", "retryable", record.attrs)
	}
	if got.Kind() != slog.KindBool {
		t.Fatalf("record[%q] kind = %v, want %v", "retryable", got.Kind(), slog.KindBool)
	}
	if !got.Bool() {
		t.Fatalf("record[%q] = %v, want true", "retryable", got.Bool())
	}
}

func TestLogAndWrap_PropagatesContextAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	ctx := WithJobID(context.Background(), "j1")
	ctx = WithRequestID(ctx, "r1")

	LogAndWrapError(ctx, logger, "sync.poll", errors.New("boom"))

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	requireCapturedAttr(t, record, "job_id", "j1")
	requireCapturedAttr(t, record, "request_id", "r1")
}

func TestLogAndWrapError_MergesExtraAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogAndWrapError(
		context.Background(),
		logger,
		"sync.poll",
		errors.New("boom"),
		slog.String("component", "worker"),
		slog.String("job_id", "job-1"),
	)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	requireCapturedAttr(t, record, "error_message", "boom")
	requireCapturedAttr(t, record, "component", "worker")
	requireCapturedAttr(t, record, "job_id", "job-1")
}

func TestLogAndWrapError_NilLoggerSafe(t *testing.T) {
	cause := errors.New("boom")

	err := LogAndWrapError(context.Background(), nil, "sync.poll", cause)

	if err == nil {
		t.Fatal("LogAndWrapError returned nil, want wrapped error")
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped error does not match cause with errors.Is")
	}
}

func TestLogAndWrapError_NilCtxFallsBackToBackground(t *testing.T) {
	var nilCtx context.Context
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogAndWrapError(nilCtx, logger, "sync.poll", errors.New("boom"))

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
