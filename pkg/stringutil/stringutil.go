// Package stringutil: 문자열 및 슬라이스 유틸리티
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

// 여러 개행 패턴을 시도하여 가장 적절한 방식으로 제거합니다.
func StripLeadingHeader(text, header string) string {
	if TrimSpace(text) == "" || TrimSpace(header) == "" {
		return text
	}
	candidates := []string{
		header + "\r\n\r\n",
		header + "\n\n",
		header + "\r\n",
		header + "\n",
		header,
	}
	for _, candidate := range candidates {
		if after, ok := strings.CutPrefix(text, candidate); ok {
			return after
		}
	}
	return text
}

// Unicode를 올바르게 처리하며, 다양한 특수문자(공백, 하이픈, 언더스코어 등)를 제거합니다.
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
