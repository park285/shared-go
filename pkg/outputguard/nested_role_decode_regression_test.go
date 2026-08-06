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

func TestGuardFailsClosedOnTripleNestedShortBase64RoleHeader(t *testing.T) {
	t.Parallel()

	// base64^3("prompt") — 모든 층이 19자 이하 short 토큰이라 nil-context 경로의
	// 1겹 unroll 한계에 걸린다. 완전 탐색 불가가 Complete로 접히면 allow 우회가 된다.
	evaluation := NewGuard().Check(CheckRequest{Text: "system WTBoS2RtSllRakE9:"})
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("evaluation = %+v, want block", evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, want decode incomplete fail-closed", evaluation.ReasonCodes)
	}
}
