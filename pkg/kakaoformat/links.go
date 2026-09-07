package kakaoformat

import (
	"regexp"
	"strings"
)

var (
	reReferenceHead = regexp.MustCompile(`!?\[([^\]]*)\]\(`)
	reLiteralURL    = regexp.MustCompile(`https?://[^\s<>]+`)
)

func renderLinks(input string, urls *store) string {
	closing := linkParentheses(input)
	code := reInlineCode.FindAllStringIndex(input, -1)
	codeIndex := 0

	var output strings.Builder

	last := 0

	for _, match := range reReferenceHead.FindAllStringSubmatchIndex(input, -1) {
		if insideCode(match[0], code, &codeIndex) {
			continue
		}

		end, ok := closing[match[1]-1]
		if !ok || match[0] < last {
			continue
		}

		label := strings.TrimSpace(input[match[2]:match[3]])
		url := strings.TrimSpace(input[match[1]:end])
		output.WriteString(input[last:match[0]])

		if label == "" || strings.EqualFold(label, url) {
			output.WriteString(urls.Put(url))
		} else {
			output.WriteString(label + "( " + urls.Put(url) + " )")
		}

		last = end + 1
	}

	output.WriteString(input[last:])

	// URL의 밑줄·별표가 이후 강조문 처리에서 다른 문자로 바뀌지 않도록 보호한다.
	return protectLiteralURLs(output.String(), urls)
}

func linkParentheses(input string) map[int]int {
	pairs := make(map[int]int)

	var stack []int

	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '\\':
			i++
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) > 0 {
				pairs[stack[len(stack)-1]] = i
				stack = stack[:len(stack)-1]
			}
		}
	}

	return pairs
}
