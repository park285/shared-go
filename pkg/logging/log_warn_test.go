package logging

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestLogWarnWithErrorAttrs_NilErrorNoops(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogWarnWithErrorAttrs(context.Background(), logger, "sync.poll.failed", "sync poll failed", nil)

	if len(handler.enabledCalls) != 0 {
		t.Fatalf("got %d Enabled calls, want 0", len(handler.enabledCalls))
	}
	if len(handler.records) != 0 {
		t.Fatalf("got %d records, want 0", len(handler.records))
	}
}

func TestLogWarnWithErrorAttrs_LogsWarnWithErrorAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)
	cause := &logAndWrapTypedError{message: "typed failure"}

	LogWarnWithErrorAttrs(context.Background(), logger, "sync.poll.failed", "sync poll failed", cause)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	if got := record.level; got != slog.LevelWarn {
		t.Fatalf("record level = %v, want %v", got, slog.LevelWarn)
	}
	requireCapturedAttr(t, record, "event", "sync.poll.failed")
	requireCapturedAttr(t, record, "error_type", "logAndWrapTypedError")
	requireCapturedAttr(t, record, "error_message", "typed failure")
}

func TestLogWarnWithErrorAttrs_MergesCallerAttrs(t *testing.T) {
	handler := newCaptureLogHandler(true)
	logger := slog.New(handler)

	LogWarnWithErrorAttrs(
		context.Background(),
		logger,
		"sync.poll.failed",
		"sync poll failed",
		errors.New("boom"),
		slog.String("channel_id", "UC123"),
		slog.Int64("room_id", 42),
	)

	if len(handler.records) != 1 {
		t.Fatalf("got %d records, want 1", len(handler.records))
	}
	record := handler.records[0]
	requireCapturedAttr(t, record, "error_message", "boom")
	requireCapturedAttr(t, record, "channel_id", "UC123")

	gotRoomID, ok := record.attrs["room_id"]
	if !ok {
		t.Fatalf("record missing %q: %#v", "room_id", record.attrs)
	}
	if gotRoomID.Kind() != slog.KindInt64 {
		t.Fatalf("record[%q] kind = %v, want %v", "room_id", gotRoomID.Kind(), slog.KindInt64)
	}
	if got := gotRoomID.Int64(); got != 42 {
		t.Fatalf("record[%q] = %d, want 42", "room_id", got)
	}
}
