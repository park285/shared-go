package outputguard

import (
	"slices"
	"testing"
)

func TestGuardBlocksNestedBase64RoleHeaderWithoutIncompleteFallback(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{Text: "system Y0hKdmJYQjA=:"})
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("evaluation = %+v, want block", evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
	if slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, nested role decode must complete", evaluation.ReasonCodes)
	}
	if !slices.Contains(evaluation.RuleIDs, "role_header_en") {
		t.Fatalf("rules = %v, want role_header_en", evaluation.RuleIDs)
	}
}
