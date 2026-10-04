package kakaoformat

import (
	"regexp"
	"strings"
)

var reDateListLine = regexp.MustCompile(`(?m)^([ \t]{0,3})(\d{4})\.([ \t]+\d{1,2}\.)`)

// escapeDateListMarkers는 줄 첫머리의 `2026. 10. 4.` 날짜가 중첩 번호 목록으로 해석되지 않도록 연도 점을 이스케이프한다.
func escapeDateListMarkers(input string) string {
	return reDateListLine.ReplaceAllString(input, `$1$2\.$3`)
}

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
