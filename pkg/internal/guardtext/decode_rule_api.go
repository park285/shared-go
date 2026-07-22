package guardtext

// HasPotentialRuleDecodeSurface는 rule decode가 필요한 변환 문법의 존재 여부를 빠르게 판정한다.
func HasPotentialRuleDecodeSurface(input string) bool {
	potential, needsNormalization := ruleDecodePreflight(input)
	if potential {
		return true
	}

	normalized := normalizedRuleDecodeInput(input, needsNormalization)

	return normalized != "" && (hasPotentialDecodeSurface(normalized) || hasPlausibleShortRuleDecodeSurface(normalized))
}

// DecodeCandidatesWithContextForRules expands standard transforms and short
// Base64/hex fragments that can contribute to a compiled rule. All paths share
// the existing decode budgets and retain fail-closed status reporting.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	semantic := decodeSemanticRuleInput(input, mayContribute)
	if semantic.status != 0 {
		return DecodeResult{Status: semantic.status}
	}
	input = semantic.projected
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	normalized := normalizedRuleDecodeInput(input, needsNormalization)
	decoded := decodeCandidatesWithContextForRules(input, normalized, mayContribute, nil, nil, originalPotential)
	if len(semantic.candidates) == 0 {
		return decoded
	}

	return mergeSemanticCandidates(semantic.candidates, decoded)
}

// DecodeCandidatesWithContextForRuleOwner avoids allocating an owner-bound
// callback when the input has no transform surface.
func DecodeCandidatesWithContextForRuleOwner[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	matcher := func(candidate string) bool { return mayContribute(owner, candidate) }
	semantic := decodeSemanticRuleInput(input, matcher)
	if semantic.status != 0 {
		return DecodeResult{Status: semantic.status}
	}
	input = semantic.projected
	originalPotential, needsNormalization := ruleDecodePreflight(input)
	if !originalPotential && !needsNormalization {
		return DecodeResult{Candidates: semantic.candidates}
	}
	normalized := normalizedRuleDecodeInput(input, needsNormalization)
	var contextMatcher EmbeddedContextMatcher
	if contextMayContribute != nil {
		contextMatcher = func(input string, start, end int, decoded string) bool {
			return contextMayContribute(owner, input, start, end, decoded)
		}
	}
	var oversizedCallback func(string, string, []string) bool
	if (len(input) > maxDecodedCandidateLen || len(normalized) > maxDecodedCandidateLen) && oversizedWouldBlock != nil {
		oversizedCallback = func(original, decoded string, bounded []string) bool {
			return oversizedWouldBlock(owner, original, decoded, bounded)
		}
	}
	decoded := decodeCandidatesWithContextForRules(
		input,
		normalized,
		matcher,
		contextMatcher,
		oversizedCallback,
		originalPotential,
	)
	if len(semantic.candidates) == 0 {
		return decoded
	}

	return mergeSemanticCandidates(semantic.candidates, decoded)
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
