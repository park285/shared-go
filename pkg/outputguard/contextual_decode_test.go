package outputguard

import (
	"encoding/base64"
	"slices"
	"testing"
)

func TestBoundGuardBlocksProtectedTextSplitAcrossEncodedFragment(t *testing.T) {
	t.Parallel()

	const protected = "internal application rules"
	bound, err := NewGuard().Bind([]string{protected})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	tests := []struct {
		name string
		text string
	}{
		{
			name: "base64 fragment",
			text: "internal " + base64.StdEncoding.EncodeToString([]byte("application rules")),
		},
		{
			name: "hex fragment",
			text: "internal hex: 61 70 70 6c 69 63 61 74 69 6f 6e 20 72 75 6c 65 73",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluation := bound.Check(test.text)
			if evaluation.Decision != DecisionBlock {
				t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
			}
			if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
				t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
			}
		})
	}
}
