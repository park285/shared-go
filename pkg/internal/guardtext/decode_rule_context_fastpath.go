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
