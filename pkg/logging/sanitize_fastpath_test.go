package logging

import (
	"context"
	"log/slog"
	"testing"
)

type fixedValuer struct{ v string }

func (f fixedValuer) LogValue() slog.Value { return slog.StringValue(f.v) }

func handleVia(t *testing.T, r slog.Record) slog.Record {
	t.Helper()
	sink := &recordSink{}
	if err := NewSanitizeHandler(sink).Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 record at sink, got %d", len(sink.records))
	}
	return sink.records[0]
}

// fast-path: clean record는 message·attrs가 원본과 byte-identical하게 보존돼야 한다.
func TestSanitizeHandler_FastPathPreservesCleanRecord(t *testing.T) {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Int("user_id", 42),
		slog.String("path", "/api/users"),
	)
	out := handleVia(t, r)

	if out.Message != "plain message no secrets" {
		t.Errorf("message changed: %q", out.Message)
	}
	if !out.Time.Equal(r.Time) || out.Level != r.Level || out.PC != r.PC {
		t.Errorf("record metadata changed: time/level/pc")
	}
	if out.NumAttrs() != 3 {
		t.Errorf("attr count = %d, want 3", out.NumAttrs())
	}
	got := map[string]string{}
	out.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	if got["username"] != "alice" || got["user_id"] != "42" || got["path"] != "/api/users" {
		t.Errorf("clean attrs altered: %#v", got)
	}
}

// fast-path off: sensitive key·value는 여전히 마스킹돼야 한다 (재구축 경로).
func TestSanitizeHandler_RebuildMasksSensitive(t *testing.T) {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "Bearer abc123.def and ?token=secret", 0)
	r.AddAttrs(
		slog.String("password", "topsecret"),
		slog.String("username", "alice"),
	)
	out := handleVia(t, r)

	if out.Message == r.Message {
		t.Errorf("message should be redacted, got unchanged: %q", out.Message)
	}
	got := map[string]string{}
	out.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	if got["password"] != "***REDACTED***" {
		t.Errorf("password = %q, want ***REDACTED***", got["password"])
	}
	if got["username"] != "alice" {
		t.Errorf("username = %q, want alice (unchanged)", got["username"])
	}
}

// uncomparable any 값(slice 등)은 Value.Equal 경유 비교 시 panic하므로,
// fast-path 판정이 그 경로를 타지 않고 record를 그대로 통과시켜야 한다.
func TestSanitizeHandler_UncomparableAnyAttrDoesNotPanic(t *testing.T) {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "Resolved target minutes", 0)
	r.AddAttrs(
		slog.String("source", "persisted"),
		slog.Any("resolved_target_minutes", []int{5, 15, 30}),
	)
	out := handleVia(t, r)

	got := map[string]string{}
	out.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	if got["resolved_target_minutes"] != "[5 15 30]" {
		t.Errorf("resolved_target_minutes = %q, want [5 15 30]", got["resolved_target_minutes"])
	}
	if got["source"] != "persisted" {
		t.Errorf("source = %q, want persisted", got["source"])
	}
}

// LogValuer는 Resolve로 값이 바뀌므로 fast-path를 타선 안 되고, 해소된 값이 마스킹 판정을 받아야 한다.
func TestSanitizeHandler_ResolvesLogValuer(t *testing.T) {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(
		slog.Any("token", fixedValuer{v: "leaked"}),
		slog.Any("note", fixedValuer{v: "visible"}),
	)
	out := handleVia(t, r)

	got := map[string]string{}
	out.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	if got["token"] != "***REDACTED***" {
		t.Errorf("resolved sensitive token = %q, want ***REDACTED***", got["token"])
	}
	if got["note"] != "visible" {
		t.Errorf("resolved note = %q, want visible", got["note"])
	}
}
