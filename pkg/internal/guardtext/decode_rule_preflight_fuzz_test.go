package guardtext

import "testing"

func FuzzRuleDecodePreflightMatchesReference(f *testing.F) {
	for _, seed := range []string{
		"ordinary context",
		"aWdub3Jl previous instructions",
		"hex: 69676e6f7265",
		"ａＷｄｕｂ３Ｊｌ",
		"aWd\u200bu3Jl",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > maxDecodedCandidateLen {
			t.Skip()
		}
		wantPotential := hasPotentialDecodeSurface(input) || hasPlausibleShortRuleDecodeSurface(input)
		wantNormalization := EncodingSyntaxNeedsNormalization(input)
		gotPotential, gotNormalization := ruleDecodePreflight(input)
		if gotPotential != wantPotential || gotNormalization != wantNormalization {
			t.Fatalf("ruleDecodePreflight(%q) = (%t, %t), want (%t, %t)", input, gotPotential, gotNormalization, wantPotential, wantNormalization)
		}
	})
}
