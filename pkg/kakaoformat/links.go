package kakaoformat

import (
	"cmp"
	"regexp"
	"slices"
)

func literalDestinations(input string) [][2]int {
	var ranges [][2]int

	closing := linkParentheses(input)

	for _, match := range reReferenceHead.FindAllStringIndex(input, -1) {
		if end, ok := closing[match[1]-1]; ok {
			ranges = append(ranges, [2]int{match[1], end})
		}
	}

	for _, match := range reLiteralURL.FindAllStringIndex(input, -1) {
		ranges = append(ranges, [2]int{match[0], match[1]})
	}

	slices.SortFunc(ranges, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })

	merged := ranges[:0]
	for _, span := range ranges {
		if len(merged) > 0 && span[0] <= merged[len(merged)-1][1] {
			merged[len(merged)-1][1] = max(merged[len(merged)-1][1], span[1])
		} else {
			merged = append(merged, span)
		}
	}

	return merged
}

var (
	reReferenceHead = regexp.MustCompile(`!?\[([^\]]*)\]\(`)
	reLiteralURL    = regexp.MustCompile(`https?://[^\s<>]+`)
)

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
