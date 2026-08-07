package guardtext

// HasPotentialRuleDecodeSurface는 rule decode가 필요한 변환 문법의 존재 여부를 빠르게 판정한다.
func HasPotentialRuleDecodeSurface(input string) bool {
	potential, needsNormalization := ruleDecodePreflight(input)
	if potential {
		return true
	}

	normalized := normalizedRuleDecodeInput(input, needsNormalization)
	if normalized != "" && (hasPotentialDecodeSurface(normalized) || hasPlausibleShortRuleDecodeSurface(normalized)) {
		return true
	}

	_, changed := normalizeSingleSpaceBase64(input)

	return changed
}

// DecodeCandidatesWithContextForRules expands standard transforms and short
// Base64/hex fragments that can contribute to a compiled rule. All paths share
// the existing decode budgets and retain fail-closed status reporting.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}
	result := decodeCandidatesForRules(input, mayContribute)
	joined, changed := normalizeSingleSpaceBase64(input)
	if !changed {
		return result
	}

	return mergeSplitBase64Readings(result, decodeCandidatesForRules(joined, mayContribute))
}

// 공백 정규화본은 원본을 대체하지 않고 두 번째 읽기로 더한다. 대체하면 " "로 나뉜 두 토큰이
// 하나로 합쳐져, 각각을 독립 조각으로 조합해 구를 만드는 기존 탐지가 사라진다.
func mergeSplitBase64Readings(original, joined DecodeResult) DecodeResult {
	if len(joined.Candidates) == 0 {
		mergeDecodeStatus(&original.Status, joined.Status)

		return original
	}

	merged := mergeSemanticCandidates(joined.Candidates, original)
	// 원본의 fail-closed는 split 토큰을 못 읽어서 선 것이다. 결합 읽기가 그 토큰을 끝까지
	// 해독했다면 미탐색 표면이 남지 않으므로 결합 읽기의 상태를 따른다.
	if joined.Complete() {
		merged.Status = joined.Status
	}

	return merged
}

func decodeCandidatesForRules(input string, mayContribute func(string) bool) DecodeResult {
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

	result := decodeForRuleOwner(input, owner, mayContribute, contextMayContribute, oversizedWouldBlock)
	joined, changed := normalizeSingleSpaceBase64(input)
	if !changed {
		return result
	}

	return mergeSplitBase64Readings(
		result,
		decodeForRuleOwner(joined, owner, mayContribute, contextMayContribute, oversizedWouldBlock),
	)
}

func decodeForRuleOwner[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
) DecodeResult {
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

// DecodeCandidatesWithContextForRuleOwnerAndBlockWitness는 일반 후보 admission과 별개로
// 실제 owner 정책에서 Block인 bounded decode 후보 하나를 보존한다. decode 한도 자체를
// 공격 근거로 쓰지 않으면서 depth·candidate 예산 뒤의 확정 공격만 차단할 때 사용한다.
func DecodeCandidatesWithContextForRuleOwnerAndBlockWitness[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
	candidateWouldBlock func(T, string) bool,
) (DecodeResult, string) {
	if mayContribute == nil || candidateWouldBlock == nil {
		return DecodeCandidatesWithContextForRuleOwner(
			input,
			owner,
			mayContribute,
			contextMayContribute,
			oversizedWouldBlock,
		), ""
	}

	blockingCandidate := ""
	wrappedMayContribute := func(owner T, candidate string) bool {
		contributes := mayContribute(owner, candidate)
		if contributes && blockingCandidate == "" && candidateWouldBlock(owner, candidate) {
			blockingCandidate = candidate
		}

		return contributes
	}

	var wrappedContextMayContribute func(T, string, int, int, string) bool
	if contextMayContribute != nil {
		wrappedContextMayContribute = func(owner T, input string, start, end int, decoded string) bool {
			contributes := contextMayContribute(owner, input, start, end, decoded)
			if !contributes || blockingCandidate != "" {
				return contributes
			}

			contextual, bounded := contextualAdmissionCandidate(input, encodedSpan{start: start, end: end}, decoded)
			if bounded && candidateWouldBlock(owner, contextual) {
				blockingCandidate = contextual
			}

			return contributes
		}
	}

	result := DecodeCandidatesWithContextForRuleOwner(
		input,
		owner,
		wrappedMayContribute,
		wrappedContextMayContribute,
		oversizedWouldBlock,
	)

	return result, blockingCandidate
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
