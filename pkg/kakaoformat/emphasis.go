package kakaoformat

import (
	"strings"
	"unicode"
)

func convertible(text string, digits bool) bool {
	for _, r := range text {
		if !convertibleRune(r, digits) {
			return false
		}
	}

	return true
}

func convertibleRune(r rune, digits bool) bool {
	switch {
	case r == ' ' || r == '\t' || r == '\n':
		return true
	case unicode.IsPunct(r) || unicode.IsSymbol(r):
		return true
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	default:
		return digits && r >= '0' && r <= '9'
	}
}

func styleBold(text string) string {
	return mapASCII(text, 0x1D5D4, 0x1D5EE, 0x1D7EC)
}

func styleItalic(text string) string {
	return mapASCII(text, 0x1D608, 0x1D622, -1)
}

func styleBoldItalic(text string) string {
	return mapASCII(text, 0x1D63C, 0x1D656, 0x1D7EC)
}

func mapASCII(text string, upper, lower, digit rune) string {
	return mapOutsidePlaceholders(text, func(part string) string {
		return mapASCIIText(part, upper, lower, digit)
	})
}

func mapASCIIText(text string, upper, lower, digit rune) string {
	var b strings.Builder

	b.Grow(len(text) * 4)

	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(upper + (r - 'A'))
		case r >= 'a' && r <= 'z':
			b.WriteRune(lower + (r - 'a'))
		case digit >= 0 && r >= '0' && r <= '9':
			b.WriteRune(digit + (r - '0'))
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

func strikeText(text string) string {
	var b strings.Builder

	b.Grow(len(text) * 3)

	for _, r := range text {
		b.WriteRune(r)
		b.WriteRune('\u0336')
	}

	return b.String()
}
