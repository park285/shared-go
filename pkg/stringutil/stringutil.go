package stringutil

import (
	"slices"
	"strings"
)

func TruncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
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
