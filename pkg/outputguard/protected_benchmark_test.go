package outputguard

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkProtectedExactCache(b *testing.B) {
	for _, count := range []int{1, maxProtectedTexts} {
		protected := makeExactBenchmarkProtectedTexts(count)
		request := CheckRequest{
			Text:           "prefix " + protected[len(protected)-1] + " suffix",
			ProtectedTexts: protected,
		}

		b.Run(fmt.Sprintf("%d/Hit", count), func(b *testing.B) {
			guard := NewGuard()
			if evaluation := guard.Check(request); evaluation.Decision != DecisionBlock {
				b.Fatalf("warm cache decision = %q, want %q", evaluation.Decision, DecisionBlock)
			}
			allocs := testing.AllocsPerRun(100, func() {
				_ = guard.Check(request)
			})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = guard.Check(request)
			}
			b.ReportMetric(allocs, "allocs/hotcheck")
		})

		b.Run(fmt.Sprintf("%d/Miss", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = NewGuard().Check(request)
			}
		})
	}
}

func BenchmarkProtectedExactMaximumOutput(b *testing.B) {
	protected := []string{"sk_test_0123456789abcdef"}
	text := strings.Repeat("a", maxOutputBytes-len(protected[0])) + protected[0]
	request := CheckRequest{Text: text, ProtectedTexts: protected}
	guard := NewGuard()
	if evaluation := guard.Check(request); evaluation.Decision != DecisionBlock {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionBlock)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = guard.Check(request)
	}
}

func BenchmarkProtectedExactCommonPrefixNoMatch(b *testing.B) {
	const commonPrefix = "alpha~beta~gamma~delta"
	protected := make([]string, maxProtectedTexts)
	for i := range protected {
		protected[i] = fmt.Sprintf("%s-secret-%02d", commonPrefix, i)
	}
	text := strings.Repeat(commonPrefix+"-public ", 256)
	request := CheckRequest{Text: text, ProtectedTexts: protected}
	guard := NewGuard()
	if evaluation := guard.Check(request); evaluation.Decision != DecisionAllow {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionAllow)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = guard.Check(request)
	}
}

func BenchmarkProtectedExactRepeatedPrefixNoMatch(b *testing.B) {
	protected := []string{strings.Repeat("ab", 32) + "z"}
	text := strings.Repeat(strings.Repeat("ab", 32)+"x", 1<<10)
	request := CheckRequest{Text: text, ProtectedTexts: protected}
	guard := NewGuard()
	if evaluation := guard.Check(request); evaluation.Decision != DecisionAllow {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionAllow)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = guard.Check(request)
	}
}

func BenchmarkProtectedExactJoinedSeparator(b *testing.B) {
	bound, err := NewGuard().Bind([]string{"internal instruction boundary"})
	if err != nil {
		b.Fatalf("Bind: %v", err)
	}
	text := strings.Repeat("ordinary!", 256) + " internal---instruction---boundary"
	if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock {
		b.Fatalf("decision = %q, want block", evaluation.Decision)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = bound.Check(text)
	}
}

func makeExactBenchmarkProtectedTexts(count int) []string {
	protected := make([]string, count)
	for i := range protected {
		protected[i] = fmt.Sprintf("protected-exact-value-%02d", i)
	}

	return protected
}
