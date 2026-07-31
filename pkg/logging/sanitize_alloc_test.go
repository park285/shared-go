package logging

import (
	"context"
	"log/slog"
	"testing"
)

// (a) isSensitiveKey는 비민감 키 반복 호출에서 alloc 0이어야 한다 (package-level 사전 상수화).
func TestIsSensitiveKey_ZeroAllocClean(t *testing.T) {
	if got := testing.AllocsPerRun(1000, func() {
		_ = isSensitiveKey("clean_key")
	}); got != 0 {
		t.Fatalf("isSensitiveKey(\"clean_key\") allocs = %v, want 0", got)
	}
}

// (b) 민감 attr·message 변경이 없는 record는 fast-path로 원본을 그대로 전달해야 한다.
// 현재 무조건 NewRecord 재구축은 record 1개당 다수 alloc을 유발하므로 이 상한에서 실패해야 한다.
func TestSanitizeHandler_CleanRecordLowAlloc(t *testing.T) {
	h := newSanitizeHandler(discardHandler{})
	ctx := context.Background()
	got := testing.AllocsPerRun(1000, func() {
		r := slog.NewRecord(testTime(), slog.LevelInfo, "plain message no secrets here", 0)
		r.AddAttrs(
			slog.String("username", "alice"),
			slog.Int("attempt", 42),
			slog.String("path", "/api/users"),
			slog.String("status", "ok"),
		)
		_ = h.Handle(ctx, r)
	})
	// fast-path는 변경 없는 record를 재구축 없이 그대로 전달하므로 Handle 추가 alloc은 0이어야 한다.
	// 재구축 경로(baseline 9 allocs)에 대한 회귀 가드.
	if got != 0 {
		t.Fatalf("clean-record Handle allocs = %v, want 0 (fast-path must not rebuild)", got)
	}
}
