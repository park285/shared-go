package promptguard

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

func BenchmarkPromptGuardBenignFastPath(b *testing.B) {
	guard := newBenchmarkGuard(b)
	input := "오늘 회의 일정과 점심 메뉴를 정리해 주세요."

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = guard.evaluateRaw(input)
	}
}

func BenchmarkPromptGuardAggregateBoundary(b *testing.B) {
	guard := newBenchmarkGuard(b)
	input := strings.Repeat("> ordinary quoted context\n\n```text\nbenign code sample\n```\n\nkey: harmless-value\n\n", 32)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = guard.evaluateRaw(input)
	}
}

func BenchmarkPromptGuardDecoderHeavy(b *testing.B) {
	guard := newBenchmarkGuard(b)
	payload := "ordinary synthetic payload that does not contain an instruction"
	input := base64.StdEncoding.EncodeToString([]byte(url.PathEscape(payload)))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = guard.evaluateRaw(input)
	}
}

func newBenchmarkGuard(b *testing.B) *Guard {
	b.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, logger)
	if err != nil {
		b.Fatalf("NewGuard() error = %v", err)
	}

	return guard
}
