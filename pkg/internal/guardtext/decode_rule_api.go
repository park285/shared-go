package guardtext

// DecodeCandidatesWithContextForRules expands standard transforms and short
// Base64/hex fragments that can contribute to a compiled rule. All paths share
// the existing decode budgets and retain fail-closed status reporting.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	input = projectOpaqueBase64ForRules(input)
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	normalized := normalizedRuleDecodeInput(input, needsNormalization)
	return decodeCandidatesWithContextForRules(input, normalized, mayContribute, nil, originalPotential)
}

// DecodeCandidatesWithContextForRuleOwner avoids allocating an owner-bound
// callback when the input has no transform surface.
func DecodeCandidatesWithContextForRuleOwner[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	input = projectOpaqueBase64ForRules(input)
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	if !originalPotential && !needsNormalization {
		return DecodeResult{}
	}
	normalized := normalizedRuleDecodeInput(input, needsNormalization)
	var oversizedCallback func(string, string, []string) bool
	if (len(input) > maxDecodedCandidateLen || len(normalized) > maxDecodedCandidateLen) && oversizedWouldBlock != nil {
		oversizedCallback = func(original, decoded string, bounded []string) bool {
			return oversizedWouldBlock(owner, original, decoded, bounded)
		}
	}
	return decodeCandidatesWithContextForRules(
		input,
		normalized,
		func(candidate string) bool { return mayContribute(owner, candidate) },
		oversizedCallback,
		originalPotential,
	)
}

func normalizedRuleDecodeInput(input string, needed bool) string {
	if !needed {
		return ""
	}
	normalized := NormalizeEncodingSyntax(input)
	if normalized == input {
		return ""
	}
	return normalized
}

// DecodedCandidateFitsBudget은 단일 decode candidate가 admission 크기 한도 안에 있는지 보고한다.
func DecodedCandidateFitsBudget(candidate string) bool {
	return len(candidate) <= maxDecodedCandidateLen
}
