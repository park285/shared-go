package outputguard

import (
	"encoding/base64"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func BenchmarkOutputGuardContextualDecodeMaximumOutput(b *testing.B) {
	const fragments = 64

	fragment := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	text := strings.Repeat("!", maxOutputBytes-fragments*(len(fragment)+1)) + strings.Repeat(fragment+"!", fragments)

	bound, err := NewGuard().Bind([]string{protected})
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

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
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

func BenchmarkOutputGuardReadableFlood(b *testing.B) {
	var builder strings.Builder

	for index := range 400 {
		builder.WriteString(base64.StdEncoding.EncodeToString([]byte("distinct readable payload " + strconv.Itoa(index))))
		builder.WriteByte(' ')
	}

	text := builder.String()

	bound, err := NewGuard().Bind([]string{longformSystemPrompt})
	if err != nil {
		b.Fatalf("Bind() error = %v", err)
	}

	if evaluation := bound.Check(text); evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		b.Fatalf("Check() evaluation = %+v, want budget-exhausted incomplete decode", evaluation)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for range b.N {
		_ = bound.Check(text)
	}
}

func BenchmarkOutputGuardStructuredCitationsWithEncodedMetadata(b *testing.B) {
	bound, err := NewGuard().Bind([]string{strings.Repeat("private marker instruction token ", 12)})
	if err != nil {
		b.Fatalf("Bind() error = %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("readable citation metadata"))
	text := structuredCitationOutput(encoded, 4)

	if evaluation := bound.Check(text); evaluation.Decision != DecisionAllow {
		b.Fatalf("Check() evaluation = %+v, want allow", evaluation)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()

	for range b.N {
		_ = bound.Check(text)
	}
}
