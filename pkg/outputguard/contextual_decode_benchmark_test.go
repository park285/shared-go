package outputguard

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

func BenchmarkOutputGuardContextualDecodeMaximumOutput(b *testing.B) {
	const fragments = 64

	fragment := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	text := strings.Repeat("!", maxOutputBytes-fragments*(len(fragment)+1)) + strings.Repeat(fragment+"!", fragments)
	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		b.Fatalf("Bind() error = %v", err)
	}
	if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		b.Fatalf("Check() evaluation = %+v, want bounded incomplete decode", evaluation)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = bound.Check(text)
	}
}

func BenchmarkOutputGuardProtectedOversizeContext(b *testing.B) {
	text := strings.Repeat("!", (8<<10)+1) + "internal IA== policy"
	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		b.Fatalf("Bind() error = %v", err)
	}
	if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		b.Fatalf("Check() evaluation = %+v, want pre-allocation byte-limit block", evaluation)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		_ = bound.Check(text)
	}
}
