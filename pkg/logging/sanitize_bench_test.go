package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func testTime() time.Time { return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) }

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }

func BenchmarkIsSensitiveKey(b *testing.B) {
	keys := []string{"clean_key", "user_id", "access_token", "request_path", "x_api_key"}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_ = isSensitiveKey(keys[i%len(keys)])
		i++
	}
}

func newCleanRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Int("user_id", 42),
		slog.String("path", "/api/users"),
		slog.String("status", "ok"),
	)
	return r
}

func newSensitiveRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "auth Bearer abc123.def456 and ?token=secret", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.String("password", "super_secret"),
		slog.String("header", "Bearer abc123.def456.ghi"),
		slog.Int("user_id", 42),
	)
	return r
}

func BenchmarkSanitizeHandlerClean(b *testing.B) {
	h := NewSanitizeHandler(discardHandler{})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = h.Handle(ctx, newCleanRecord())
	}
}

func BenchmarkSanitizeHandlerSensitive(b *testing.B) {
	h := NewSanitizeHandler(discardHandler{})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = h.Handle(ctx, newSensitiveRecord())
	}
}
