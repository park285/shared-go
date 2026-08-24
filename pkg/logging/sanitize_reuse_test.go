package logging

import (
	"log/slog"
	"testing"
)

// Handle은 변경 감지 패스의 산출물을 재사용하고 그 앞 attr은 원본을 그대로 통과시킨다.
// 이 최적화가 "정제를 건너뛴" 것과 구분되지 않으면 유출이 되므로, 전량 재정제 결과와
// 항상 같은 record가 나오는지 고정한다.
func TestSanitizeHandler_ReuseMatchesFullSanitization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func() slog.Record
	}{
		{name: "clean", build: newCleanRecord},
		{name: "message secret only", build: newSensitiveRecord},
		{name: "clean group before nothing", build: newGroupNoSecretRecord},
		{name: "secret inside group after clean attrs", build: newGroupWithSecretRecord},
		{name: "privacy keys", build: newPrivacyRecord},
		{name: "privacy map", build: newPrivacyMapRecord},
		{name: "wide clean", build: func() slog.Record { return newWideRecord(false) }},
		{name: "wide privacy last", build: func() slog.Record { return newWideRecord(true) }},
		{
			name: "secret in final attr after many clean attrs",
			build: func() slog.Record {
				r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
				r.AddAttrs(
					slog.String("a", "1"),
					slog.Group("meta", slog.String("method", "GET"), slog.Int("code", 200)),
					slog.String("b", "2"),
					slog.String("authorization", "Bearer abc123.def456.ghi"),
					slog.String("c", "3"),
				)

				return r
			},
		},
		{
			name: "logvaluer after clean attrs",
			build: func() slog.Record {
				r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
				r.AddAttrs(
					slog.String("a", "1"),
					slog.Any("lv", secretValuer{}),
					slog.String("password", "hunter2"),
				)

				return r
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordSink{}
			if err := newSanitizeHandler(sink).Handle(t.Context(), tt.build()); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			if len(sink.records) != 1 {
				t.Fatalf("captured %d records, want 1", len(sink.records))
			}

			want := fullySanitized(tt.build())
			assertRecordEqual(t, sink.records[0], want)
		})
	}
}

type secretValuer struct{}

func (secretValuer) LogValue() slog.Value { return slog.StringValue("Bearer abc123.def456.ghi") }

func fullySanitized(record slog.Record) slog.Record {
	out := slog.NewRecord(record.Time, record.Level, redactSecrets(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		out.AddAttrs(sanitizeAttr(attr))

		return true
	})

	return out
}

func assertRecordEqual(t *testing.T, got, want slog.Record) {
	t.Helper()

	if got.Message != want.Message {
		t.Fatalf("message = %q, want %q", got.Message, want.Message)
	}

	if got.NumAttrs() != want.NumAttrs() {
		t.Fatalf("attr count = %d, want %d", got.NumAttrs(), want.NumAttrs())
	}

	gotAttrs := collectAttrStrings(got)
	wantAttrs := collectAttrStrings(want)

	for i := range wantAttrs {
		if gotAttrs[i] != wantAttrs[i] {
			t.Fatalf("attr[%d] = %s, want %s\nall got:  %v\nall want: %v", i, gotAttrs[i], wantAttrs[i], gotAttrs, wantAttrs)
		}
	}
}

func collectAttrStrings(record slog.Record) []string {
	out := make([]string, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		out = append(out, attr.String())
		return true
	})

	return out
}
