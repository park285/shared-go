package kakaoformat

import (
	"regexp"
	"strings"
)

var (
	reImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reLink  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func renderLinks(input string) string {
	text := reImage.ReplaceAllStringFunc(input, flattenRef)
	return reLink.ReplaceAllStringFunc(text, flattenRef)
}

func flattenRef(match string) string {
	parts := reImage.FindStringSubmatch(match)
	if parts == nil {
		parts = reLink.FindStringSubmatch(match)
	}

	if len(parts) < 3 {
		return match
	}

	label := strings.TrimSpace(parts[1])
	url := strings.TrimSpace(parts[2])

	if label == "" || strings.EqualFold(label, url) {
		return url
	}

	return label + "( " + url + " )"
}
