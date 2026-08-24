package outputguard

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkProtectedExactCache(b *testing.B) {
	for _, count := range []int{1, maxProtectedTexts} {
		protected := makeExactBenchmarkProtectedTexts(count)
		text := "prefix " + protected[len(protected)-1] + " suffix"

		b.Run(fmt.Sprintf("%d/Hit", count), func(b *testing.B) {
			bound, err := NewGuard().Bind(protected)
			if err != nil {
				b.Fatalf("Bind: %v", err)
			}

			if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock {
				b.Fatalf("warm index decision = %q, want %q", evaluation.Decision, DecisionBlock)
			}

			allocs := testing.AllocsPerRun(100, func() {
				_ = bound.Check(text)
			})

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				_ = bound.Check(text)
			}

			b.ReportMetric(allocs, "allocs/hotcheck")
		})

		b.Run(fmt.Sprintf("%d/Miss", count), func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				bound, err := NewGuard().Bind(protected)
				if err != nil {
					b.Fatal(err)
				}

				_ = bound.Check(text)
			}
		})
	}
}

func BenchmarkProtectedExactMaximumOutput(b *testing.B) {
	protected := []string{"sk_test_0123456789abcdef"}
	text := strings.Repeat("a", maxOutputBytes-len(protected[0])) + protected[0]

	bound, err := NewGuard().Bind(protected)
	if err != nil {
		b.Fatalf("Bind: %v", err)
	}

	if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionBlock)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for range b.N {
		_ = bound.Check(text)
	}
}

func BenchmarkProtectedExactCommonPrefixNoMatch(b *testing.B) {
	const commonPrefix = "alpha~beta~gamma~delta"

	protected := make([]string, maxProtectedTexts)

	for i := range protected {
		protected[i] = fmt.Sprintf("%s-secret-%02d", commonPrefix, i)
	}

	text := strings.Repeat(commonPrefix+"-public ", 256)

	bound, err := NewGuard().Bind(protected)
	if err != nil {
		b.Fatalf("Bind: %v", err)
	}

	if evaluation := bound.Check(text); evaluation.Decision != DecisionAllow {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionAllow)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for range b.N {
		_ = bound.Check(text)
	}
}

func BenchmarkProtectedExactRepeatedPrefixNoMatch(b *testing.B) {
	protected := []string{strings.Repeat("ab", 32) + "z"}
	text := strings.Repeat(strings.Repeat("ab", 32)+"x", 1<<10)

	bound, err := NewGuard().Bind(protected)
	if err != nil {
		b.Fatalf("Bind: %v", err)
	}

	if evaluation := bound.Check(text); evaluation.Decision != DecisionAllow {
		b.Fatalf("decision = %q, want %q", evaluation.Decision, DecisionAllow)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for range b.N {
		_ = bound.Check(text)
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

func BenchmarkProtectedExactLongSeparatorRun(b *testing.B) {
	bound, err := NewGuard().Bind([]string{"internal boundary"})
	if err != nil {
		b.Fatalf("Bind: %v", err)
	}

	for _, separatorBytes := range []int{64, 4 << 10, 64 << 10} {
		text := "internal" + strings.Repeat("-", separatorBytes) + "boundary"
		b.Run(fmt.Sprintf("%d", separatorBytes), func(b *testing.B) {
			if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock {
				b.Fatalf("decision = %q, want block", evaluation.Decision)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			b.ResetTimer()

			for range b.N {
				_ = bound.Check(text)
			}
		})
	}
}

func makeExactBenchmarkProtectedTexts(count int) []string {
	protected := make([]string, count)
	for i := range protected {
		protected[i] = fmt.Sprintf("protected-exact-value-%02d", i)
	}

	return protected
}
