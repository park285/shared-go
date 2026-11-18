package guardtext

type ruleDecodeProof struct {
	inputCannotContribute func(string) bool
	work                  *protectedDecodeWork
}

func (proof ruleDecodeProof) excludes(input, normalized string) bool {
	return proof.inputCannotContribute != nil && proof.inputCannotContribute(input) &&
		(normalized == "" || proof.inputCannotContribute(normalized))
}

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

func mergeSplitBase64Readings(original, joined DecodeResult) DecodeResult {
	if len(joined.Candidates) == 0 {
		mergeDecodeStatus(&original.Status, joined.Status)

		return original
	}

	merged := mergeSemanticCandidates(joined.Candidates, original)
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
	decoded := decodeCandidatesWithContextForRules(input, normalized, mayContribute, nil, nil, originalPotential, ruleDecodeProof{})

	if len(semantic.candidates) == 0 {
		return decoded
	}

	return mergeSemanticCandidates(semantic.candidates, decoded)
}

func decodeCandidatesWithContextForRuleOwner[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
	proof ruleDecodeProof,
) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}

	result := decodeForRuleOwner(input, owner, mayContribute, contextMayContribute, oversizedWouldBlock, proof)
	joined, changed := normalizeSingleSpaceBase64(input)

	if !changed {
		return result
	}

	return mergeSplitBase64Readings(
		result,
		decodeForRuleOwner(joined, owner, mayContribute, contextMayContribute, oversizedWouldBlock, proof),
	)
}

func decodeForRuleOwner[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
	proof ruleDecodeProof,
) DecodeResult {
	if proof.work != nil {
		var status DecodeStatus

		if !consumeProtectedContextWork(proof.work, &status, len(input)) {
			return DecodeResult{Status: status}
		}
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
		proof,
	)

	if len(semantic.candidates) == 0 {
		return decoded
	}

	return mergeSemanticCandidates(semantic.candidates, decoded)
}

func DecodeCandidatesWithContextForRuleOwnerAndBlockWitness[T any](
	input string,
	owner T,
	mayContribute func(T, string) bool,
	contextMayContribute func(T, string, int, int, string) bool,
	oversizedWouldBlock func(T, string, string, []string) bool,
	candidateWouldBlock func(T, string) bool,
	inputCannotContribute func(T, string) bool,
	budget *RuleRegionDecoder,
) (DecodeResult, string) {
	if mayContribute == nil || candidateWouldBlock == nil {
		return decodeCandidatesWithContextForRuleOwner(
			input,
			owner,
			mayContribute,
			contextMayContribute,
			oversizedWouldBlock,
			ruleDecodeProof{},
		), ""
	}

	var proof ruleDecodeProof

	if budget != nil {
		budget.work.regionInteriors = true
		proof.work = &budget.work
	}

	if inputCannotContribute != nil && budget == nil {
		proof.inputCannotContribute = func(candidate string) bool {
			return inputCannotContribute(owner, candidate)
		}
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

	result := decodeCandidatesWithContextForRuleOwner(
		input,
		owner,
		wrappedMayContribute,
		wrappedContextMayContribute,
		oversizedWouldBlock,
		proof,
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

func DecodedCandidateFitsBudget(candidate string) bool {
	return len(candidate) <= maxDecodedCandidateLen
}
