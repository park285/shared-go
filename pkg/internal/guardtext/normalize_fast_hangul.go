package guardtext

import (
	"strings"
	"unicode/utf8"
)

func writeNormalizedASCII(normalized *strings.Builder, text string, trimEdges, pendingSpace bool) bool {
	for i := range len(text) {
		replacement := normalizeASCIIReplacement[text[i]]

		if replacement == "" {
			continue
		}

		if replacement == " " {
			pendingSpace = normalized.Len() > 0 || !trimEdges
			continue
		}

		if pendingSpace {
			normalized.WriteByte(' ')

			pendingSpace = false
		}

		normalized.WriteString(replacement)
	}

	return pendingSpace
}

func normalizeFastHangulASCIIText(text string, trimEdges bool) (string, bool) {
	for _, value := range text {
		if value >= utf8.RuneSelf && (value < 0xAC00 || value > 0xD7A3) {
			return "", false
		}
	}

	var normalized strings.Builder

	normalized.Grow(len(text))

	pendingSpace := false

	for rest := text; rest != ""; {
		ascii := len(rest)
		if index := strings.IndexFunc(rest, func(value rune) bool { return value >= utf8.RuneSelf }); index >= 0 {
			ascii = index
		}

		pendingSpace = writeNormalizedASCII(&normalized, foldDisguisedCapitalI(rest[:ascii]), trimEdges, pendingSpace)
		rest = rest[ascii:]

		for rest != "" && rest[0] >= utf8.RuneSelf {
			if pendingSpace {
				normalized.WriteByte(' ')

				pendingSpace = false
			}

			_, size := utf8.DecodeRuneInString(rest)
			normalized.WriteString(rest[:size])

			rest = rest[size:]
		}
	}

	if pendingSpace && !trimEdges {
		normalized.WriteByte(' ')
	}

	return normalized.String(), true
}
