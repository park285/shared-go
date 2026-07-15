package guardtext

// HasPotentialRuleDecodeSurface reports whether rule-aware decoding can produce
// any supported standard or short contextual surface for input.
func HasPotentialRuleDecodeSurface(input string) bool {
	return len(ruleDecodeRoots(input)) > 0
}
