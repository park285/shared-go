package guardtext

// HasPotentialRuleDecodeSurface reports whether rule-aware decoding can produce
// any supported standard or short contextual surface for input.
func HasPotentialRuleDecodeSurface(input string) bool {
	if hasRuleDecodeSurface(input) {
		return true
	}
	if !needsRuleEncodingSyntaxNormalization(input) {
		return false
	}

	normalized := NormalizeEncodingSyntax(input)
	return normalized != input && hasRuleDecodeSurface(normalized)
}

// DecodeStandaloneBase64RuleFastPath returns the ordinary bounded decode result
// when an exact standalone Base64 surface has no nested short rule fragment.
// handled is false when the rule-aware matcher must continue composition.
func DecodeStandaloneBase64RuleFastPath(input string) (result DecodeResult, handled bool) {
	potential, standalone := classifyPotentialDecodeSurface(input)
	if !potential || !standalone {
		return DecodeResult{}, false
	}

	result = DecodeCandidates(input)
	if !result.Complete() {
		return result, true
	}
	if len(result.Candidates) == 0 || decodeCandidatesContainShortRuleSurface(result.Candidates) {
		return DecodeResult{}, false
	}
	return result, true
}
