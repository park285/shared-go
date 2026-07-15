package promptguard

import (
	"slices"
	"testing"
)

func TestEmbeddedRulepackHardeningCorpusV3(t *testing.T) {
	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, nil)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}

	for _, tc := range readCorpusCases(t, "testdata/corpus-v3-hardening.jsonl") {
		t.Run(tc.ID, func(t *testing.T) {
			evaluation := evaluateForTest(t, guard, tc.Input)
			if evaluation.Decision != tc.ExpectedDecision {
				t.Fatalf("decision = %q, want %q (hits=%v)", evaluation.Decision, tc.ExpectedDecision, matchedRuleIDs(evaluation.Hits))
			}
			actualRules := matchedRuleIDs(evaluation.Hits)
			for _, expected := range tc.ExpectedRules {
				if !slices.Contains(actualRules, expected) {
					t.Errorf("rules = %v, want %q", actualRules, expected)
				}
			}
		})
	}
}
