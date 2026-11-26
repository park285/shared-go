package kakaoformat

import (
	"strings"
	"unicode/utf8"
)

func Render(input string) string {
	if strings.TrimSpace(input) == "" || strings.ContainsRune(input, 0) || !utf8.ValidString(input) {
		return input
	}

	return render(input)
}
