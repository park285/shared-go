package guardtext

// DecodeCandidatesWithContextForRules expands standard transforms and short
// Base64/hex fragments that can contribute to a compiled rule. All paths share
// the existing decode budgets and retain fail-closed status reporting.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	return decodeCandidatesWithContextForRules(input, mayContribute, originalPotential, needsNormalization)
}

// DecodeCandidatesWithContextForRuleOwner avoids allocating an owner-bound
// callback when the input has no transform surface.
func DecodeCandidatesWithContextForRuleOwner[T any](input string, owner T, mayContribute func(T, string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	if !originalPotential && !needsNormalization {
		return DecodeResult{}
	}
	return decodeCandidatesWithContextForRules(
		input,
		func(candidate string) bool { return mayContribute(owner, candidate) },
		originalPotential,
		needsNormalization,
	)
}

func ruleDecodePreflight(input string) (bool, bool) {
	return hasPotentialDecodeSurface(input) || hasPlausibleShortRuleDecodeSurface(input), EncodingSyntaxNeedsNormalization(input)
}

// DecodedCandidateFitsBudget은 단일 decode candidate가 검사 가능한 크기인지 보고한다.
func DecodedCandidateFitsBudget(candidate string) bool {
	return len(candidate) <= maxDecodedCandidateLen
}
