package logging

import (
	"log/slog"
	"testing"
)

type fixedValuer struct{ v string }

func (f fixedValuer) LogValue() slog.Value { return slog.StringValue(f.v) }

func handleVia(t *testing.T, r slog.Record) slog.Record {
	t.Helper()

	sink := &recordSink{}
	if err := newSanitizeHandler(sink).Handle(t.Context(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(sink.records) != 1 {
		t.Fatalf("expected 1 record at sink, got %d", len(sink.records))
	}

	return sink.records[0]
}

func TestSanitizeHandler_FastPathPreservesCleanRecord(t *testing.T) {
	r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets", 0)
	r.AddAttrs(
		slog.String("username", "alice"),
		slog.Int("attempt", 42),
		slog.String("path", "/api/users"),
	)

	out := handleVia(t, r)

	if out.Message != "plain message no secrets" {
		t.Errorf("message changed: %q", out.Message)
	}

	if !out.Time.Equal(r.Time) || out.Level != r.Level || out.PC != r.PC {
		t.Error("record metadata changed: time/level/pc")
	}

	if out.NumAttrs() != 3 {
		t.Errorf("attr count = %d, want 3", out.NumAttrs())
	}

	got := map[string]string{}

	out.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})

	if got["username"] != "alice" || got["attempt"] != "42" || got["path"] != "/api/users" {
		t.Errorf("clean attrs altered: %#v", got)
	}
}

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

	if got["resolved_target_minutes"] != blockedUnsupported {
		t.Errorf("resolved_target_minutes = %q, want %q", got["resolved_target_minutes"], blockedUnsupported)
	}

	if got["source"] != "persisted" {
		t.Errorf("source = %q, want persisted", got["source"])
	}
}

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
