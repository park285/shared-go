package outputguard

import (
	"slices"
	"testing"
)

func TestBoundGuardBlocksHexPayloadInsideStructuredJSON(t *testing.T) {
	t.Parallel()

	guard, err := NewGuard().Bind([]string{"internal boundary"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	raw := `{"scale":"hex: 69 6e 74 65 72 6e 61 6c 2d 2d 2d 2d 2d 62 6f 75 6e 64 61 72 79"}`
	evaluation := guard.Check(raw)
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("Check() decision = %q, reasons=%v, want %q", evaluation.Decision, evaluation.ReasonCodes, DecisionBlock)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("Check() reasons = %v, want %q", evaluation.ReasonCodes, ReasonProtectedTextOverlap)
	}
}
