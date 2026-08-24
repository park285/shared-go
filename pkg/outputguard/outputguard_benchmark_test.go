package outputguard

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkOutputGuardBaseline(b *testing.B) {
	text := "요청하신 내용을 바탕으로 일반적인 답변을 작성했습니다."
	guard := NewGuard()
	request := CheckRequest{Text: text}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = guard.Check(request)
	}
}

func BenchmarkOutputGuardProtectedOverlap(b *testing.B) {
	protected := []string{makeTokenBoundaryText(protectedTokenWindow+8, protectedMinRunes+80)}
	text := "prefix " + protected[0] + " suffix"
	guard := NewGuard()

	if _, err := guard.Bind(protected); err != nil {
		b.Fatalf("Bind: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		bound, err := guard.Bind(protected)
		if err != nil {
			b.Fatal(err)
		}

		_ = bound.Check(text)
	}
}

func BenchmarkOutputGuardProtectedIndex(b *testing.B) {
	protected := make([]string, maxProtectedTexts)
	for i := range protected {
		protected[i] = makeTokenBoundaryText(protectedTokenWindow+8, protectedMinRunes+80+i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = buildProtectedIndex(protected)
	}
}

func BenchmarkOutputGuardExactNoMatch(b *testing.B) {
	index := buildProtectedIndex([]string{"protected exact benchmark value"})
	text := strings.Repeat("ordinary benchmark response ", 256)

	if index.overlapsText(text) {
		b.Fatal("overlaps() = true, want false")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = index.overlapsText(text)
	}
}

func BenchmarkOutputGuardExactCommonPrefix(b *testing.B) {
	const commonPrefix = "alpha~beta~gamma~delta"

	protected := make([]string, maxProtectedTexts)

	for i := range protected {
		protected[i] = fmt.Sprintf("%s-secret-%02d", commonPrefix, i)
	}

	index := buildProtectedIndex(protected)
	text := strings.Repeat(commonPrefix+"-public ", 256)

	if index.overlapsText(text) {
		b.Fatal("overlaps() = true, want false")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = index.overlapsText(text)
	}
}

func BenchmarkOutputGuardExactRepeatedPrefix(b *testing.B) {
	index := buildProtectedIndex([]string{strings.Repeat("ab", 32) + "z"})
	text := strings.Repeat(strings.Repeat("ab", 32)+"x", 1<<10)

	if index.overlapsText(text) {
		b.Fatal("overlaps() = true, want false")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = index.overlapsText(text)
	}
}
