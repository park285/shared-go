package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func testTime() time.Time { return time.Date(2026, time.June, 11, 0, 0, 0, 0, time.UTC) }

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }

func BenchmarkIsSensitiveKey(b *testing.B) {
	keys := []string{"clean_key", "attempt", "access_token", "request_path", "x_api_key"}

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
		slog.Int("attempt", 42),
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
		slog.Int("attempt", 42),
	)

	return r
}

func BenchmarkSanitizeHandlerClean(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newCleanRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerSensitive(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newSensitiveRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func newGroupNoSecretRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Group("request",
			slog.String("method", "GET"),
			slog.String("path", "/api/users"),
			slog.Int("status", 200),
		),
		slog.Int("attempt", 42),
	)

	return r
}

func newGroupWithSecretRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Group("request",
			slog.String("method", "GET"),
			slog.String("path", "/api/users"),
			slog.String("authorization", "Bearer abc123.def456.ghi"),
		),
		slog.Int("attempt", 42),
	)

	return r
}

func BenchmarkSanitizeHandlerGroupNoSecret(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newGroupNoSecretRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func newPrivacyRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Int64("room_name", 8842),
		slog.String(tokenUserName, "u-8842"),
		slog.String("path", "/api/users"),
	)

	return r
}

func newPrivacyMapRecord() slog.Record {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Any("payload", map[string]any{tokenUserName: "u-8842", testVideoID: testVid1, testCount: 3}),
	)

	return r
}

// slog.Record는 attr 5개까지 inline이라 그 이하에서는 재구축 비용이 alloc으로 드러나지 않는다.
func newWideRecord(privacy bool) slog.Record {
	idKey := testVideoID

	if privacy {
		idKey = tokenUserName
	}

	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.String("path", "/api/users"),
		slog.String("status", "ok"),
		slog.String("method", "GET"),
		slog.Int("attempt", 1),
		slog.String("service", "bot"),
		slog.String(idKey, "id-8842"),
	)

	return r
}

func BenchmarkSanitizeHandlerWideClean(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newWideRecord(false)); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerWidePrivacy(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newWideRecord(true)); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerPrivacyKeys(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newPrivacyRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerPrivacyMap(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newPrivacyMapRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerPrivacyGroup(b *testing.B) {
	h := newSanitizeHandler(discardHandler{}).WithGroup("sender")
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
		r.AddAttrs(slog.String("name", "alice"), slog.Int64("id", 8842))

		if err := h.Handle(ctx, r); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}

func BenchmarkSanitizeHandlerGroupWithSecret(b *testing.B) {
	h := newSanitizeHandler(discardHandler{})
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		if err := h.Handle(ctx, newGroupWithSecretRecord()); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}
