package kakaoformat

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var reStrike = regexp.MustCompile(`~~(.*?)~~`)

func renderEmphasis(input string) string {
	text := replaceDelimited(input, "***", func(content string) string {
		return styleSpan(content, "❮", "❯", styleBoldItalic, true)
	})
	text = replaceDelimited(text, "___", func(content string) string {
		return styleSpan(content, "❮", "❯", styleBoldItalic, true)
	})
	text = replaceDelimited(text, "**", func(content string) string {
		return styleSpan(content, "❪", "❫", styleBold, true)
	})
	text = replaceDelimited(text, "__", func(content string) string {
		return styleSpan(content, "❪", "❫", styleBold, true)
	})
	text = replaceDelimited(text, "*", func(content string) string {
		return styleSpan(content, "❬", "❭", styleItalic, false)
	})

	return replaceDelimited(text, "_", func(content string) string {
		return styleSpan(content, "❬", "❭", styleItalic, false)
	})
}

func styleSpan(content, open, end string, convert func(string) string, digits bool) string {
	converted := convert(content)
	if convertible(content, digits) {
		return converted
	}

	return open + converted + end
}

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

func replaceDelimited(input, delim string, transform func(string) string) string {
	if delim == "" {
		return input
	}

	var b strings.Builder
	index := 0

	for index < len(input) {
		start := findDelim(input, delim, index, true)
		if start < 0 {
			b.WriteString(input[index:])
			break
		}

		end := findDelim(input, delim, start+len(delim), false)
		if end < 0 {
			b.WriteString(input[index:])
			break
		}

		b.WriteString(input[index:start])
		b.WriteString(transform(input[start+len(delim) : end]))
		index = end + len(delim)
	}

	return b.String()
}

func findDelim(input, delim string, start int, opener bool) int {
	search := start
	for search < len(input) {
		offset := strings.Index(input[search:], delim)
		if offset < 0 {
			return -1
		}

		pos := search + offset
		if opener {
			if isOpener(input, pos, len(delim)) {
				return pos
			}
		} else if isCloser(input, pos, len(delim)) {
			return pos
		}

		search = pos + len(delim)
	}

	return -1
}

func isOpener(input string, pos, width int) bool {
	prev := prevRune(input, pos)
	next := nextRune(input, pos+width)
	if next == 0 || unicode.IsSpace(next) {
		return false
	}

	return prev == 0 || unicode.IsSpace(prev) || unicode.IsPunct(prev) || unicode.IsSymbol(prev)
}

func isCloser(input string, pos, width int) bool {
	prev := prevRune(input, pos)
	next := nextRune(input, pos+width)
	if prev == 0 || unicode.IsSpace(prev) {
		return false
	}

	return next == 0 || unicode.IsSpace(next) || unicode.IsPunct(next) || unicode.IsSymbol(next)
}

func prevRune(text string, byteIndex int) rune {
	if byteIndex > len(text) {
		byteIndex = len(text)
	}
	if byteIndex <= 0 {
		return 0
	}

	r, size := utf8.DecodeLastRuneInString(text[:byteIndex])
	if size == 0 {
		return 0
	}

	return r
}

func nextRune(text string, byteIndex int) rune {
	if byteIndex >= len(text) {
		return 0
	}
	for _, r := range text[byteIndex:] {
		return r
	}

	return 0
}

func renderStrike(input string) string {
	return reStrike.ReplaceAllStringFunc(input, func(match string) string {
		parts := reStrike.FindStringSubmatch(match)

		var b strings.Builder
		b.Grow(len(parts[1]) * 3)
		for _, r := range parts[1] {
			b.WriteRune(r)
			b.WriteRune('\u0336')
		}

		return b.String()
	})
}
