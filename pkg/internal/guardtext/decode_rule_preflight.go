package guardtext

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func ruleDecodePreflight(input string) (bool, bool) {
	potential := false
	needsNormalization := false
	requiresCompatibilityCheck := false

	for index := 0; index < len(input); {
		value := input[index]
		if isBase64Char(value) {
			index, potential = scanRuleBase64Candidate(input, index, potential)
			continue
		}

		if isExplicitRuleDecodeSyntax(value) {
			potential = true
		}

		if value < utf8.RuneSelf {
			needsNormalization = needsNormalization || value < ' ' || value == 0x7f
			index++

			continue
		}

		var runeNeedsNormalization, runeNeedsCompatibility bool

		index, runeNeedsNormalization, runeNeedsCompatibility = scanRuleNonASCII(input, index)
		needsNormalization = needsNormalization || runeNeedsNormalization
		requiresCompatibilityCheck = requiresCompatibilityCheck || runeNeedsCompatibility
	}

	if !needsNormalization && requiresCompatibilityCheck {
		needsNormalization = norm.NFKC.QuickSpanString(input) != len(input)
	}

	return potential, needsNormalization
}

func scanRuleBase64Candidate(input string, index int, potential bool) (int, bool) {
	start := index
	for index < len(input) && isBase64Char(input[index]) {
		if !potential && hasASCIIFoldHexAt(input, index) {
			potential = true
		}

		index++
	}

	for padding := 0; index < len(input) && input[index] == '=' && padding < 2; padding++ {
		index++
	}

	if !potential && plausibleRuleDecodeCandidate(input[start:index]) {
		potential = true
	}

	return index, potential
}

func scanRuleNonASCII(input string, index int) (int, bool, bool) {
	decoded, size := utf8.DecodeRuneInString(input[index:])
	needsNormalization := unicode.Is(unicode.Cf, decoded) || unicode.Is(unicode.Cc, decoded)
	needsCompatibility := decoded < 0xAC00 || decoded > 0xD7A3

	return index + size, needsNormalization, needsCompatibility
}

func isExplicitRuleDecodeSyntax(value byte) bool {
	return value == '%' || value == '&' || value == '\\'
}

func plausibleRuleDecodeCandidate(candidate string) bool {
	if len(candidate) >= minBase64CandidateLen {
		return true
	}

	return len(candidate) >= 4 && plausibleShortBase64Value(candidate)
}

func hasASCIIFoldHexAt(input string, index int) bool {
	if index+2 >= len(input) {
		return false
	}

	return asciiLower(input[index]) == 'h' && asciiLower(input[index+1]) == 'e' && asciiLower(input[index+2]) == 'x'
}

func asciiLower(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}

	return value
}
