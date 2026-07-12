package outputguard

import "testing"

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
	protected := makeTokenBoundaryText(protectedTokenWindow+8, protectedMinRunes+80)
	request := CheckRequest{Text: "prefix " + protected + " suffix", ProtectedTexts: []string{protected}}
	guard := NewGuard()
	_ = guard.Check(request)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = guard.Check(request)
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
