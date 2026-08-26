package stringutil

import (
	"slices"
	"strings"
	"unicode/utf8"
)

func TruncateString(s string, maxRunes int) string {
	if maxRunes < 0 {
		panic("maxRunes must be non-negative")
	}

	if len(s) <= maxRunes {
		return s
	}

	runeCount := 0
	prefixHasInvalidUTF8 := false

	for byteIndex, current := range s {
		if runeCount == maxRunes {
			prefix := s[:byteIndex]

			if prefixHasInvalidUTF8 {
				return string([]rune(prefix)) + "..."
			}

			return prefix + "..."
		}

		if current == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[byteIndex:])

			prefixHasInvalidUTF8 = prefixHasInvalidUTF8 || size == 1
		}

		runeCount++
	}

	return s
}

func ContainsString(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func NormalizeKey(s string) string {
	s = Normalize(s)
	if s == "" {
		return ""
	}

	var builder strings.Builder

	for _, r := range s {
		switch r {
		case ' ', '-', '_', '.', '!', '☆', '・', '\u2018', '\u2019', '\'', 'ー', '—':
			continue
		default:
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// 공백은 하이픈(-)으로 변환하고, 특정 특수문자를 제거합니다.
func Slugify(s string) string {
	s = Normalize(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "!", "")

	return s
}
