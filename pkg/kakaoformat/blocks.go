package kakaoformat

import (
	"strings"
)

func listIndent(level int) string {
	return strings.Repeat("  ", level)
}

func bulletFor(level int) string {
	switch {
	case level <= 0:
		return "⦁"
	case level == 1:
		return "￮"
	case level == 2:
		return "▸"
	default:
		return "▹"
	}
}
