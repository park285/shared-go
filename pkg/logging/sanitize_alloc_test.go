package logging

import (
	"log/slog"
	"testing"
)

func TestIsSensitiveKey_ZeroAllocClean(t *testing.T) {
	if got := testing.AllocsPerRun(1000, func() {
		_ = isSensitiveKey("clean_key")
	}); got != 0 {
		t.Fatalf("isSensitiveKey(\"clean_key\") allocs = %v, want 0", got)
	}
}

func TestSanitizeHandler_CleanRecordLowAlloc(t *testing.T) {
	h := newSanitizeHandler(discardHandler{})
	ctx := t.Context()
	got := testing.AllocsPerRun(1000, func() {
		r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
		r.AddAttrs(
			slog.String("username", "alice"),
			slog.Int("attempt", 42),
			slog.String("path", "/api/users"),
			slog.String("status", "ok"),
		)

		if err := h.Handle(ctx, r); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	})
	// fast-path는 변경 없는 record를 재구축 없이 그대로 전달하므로 Handle 추가 alloc은 0이어야 한다.
	// 재구축 경로(baseline 9 allocs)에 대한 회귀 가드.
	if got != 0 {
		t.Fatalf("clean-record Handle allocs = %v, want 0 (fast-path must not rebuild)", got)
	}
}
