package promptguard

import (
	"encoding/base64"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

var benchmarkCacheKeySink string

func BenchmarkPromptGuardCacheKey(b *testing.B) {
	input := strings.Repeat("prompt-guard-input-", 64)

	b.ReportAllocs()

	for b.Loop() {
		benchmarkCacheKeySink = cacheKey(input)
	}
}

func BenchmarkPromptGuardBenignFastPath(b *testing.B) {
	guard := newBenchmarkGuard(b)
	input := "오늘 회의 일정과 점심 메뉴를 정리해 주세요."

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = guard.evaluateRaw(input)
	}
}

func BenchmarkPromptGuardAggregateBoundary(b *testing.B) {
	guard := newBenchmarkGuard(b)
	input := strings.Repeat("> ordinary quoted context\n\n```text\nbenign code sample\n```\n\nkey: harmless-value\n\n", 32)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = guard.evaluateRaw(input)
	}
}

func BenchmarkPromptGuardDecoderHeavy(b *testing.B) {
	guard := newBenchmarkGuard(b)
	payload := "ordinary synthetic payload that does not contain an instruction"
	input := base64.StdEncoding.EncodeToString([]byte(url.PathEscape(payload)))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = guard.evaluateRaw(input)
	}
}

func newBenchmarkGuard(b *testing.B) *Guard {
	b.Helper()

	logger := slog.New(slog.DiscardHandler)

	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, logger)
	if err != nil {
		b.Fatalf("NewGuard() error = %v", err)
	}

	return guard
}
