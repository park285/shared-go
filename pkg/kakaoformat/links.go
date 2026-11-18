package kakaoformat

import (
	"cmp"
	"slices"
	"strings"
)

func literalDestinations(input string) [][2]int {
	var (
		ranges  [][2]int
		closing map[int]int
	)

	for from := 0; ; {
		head := referenceHeadEnd(input, from)
		if head < 0 {
			break
		}

		if closing == nil {
			closing = linkParentheses(input)
		}

		if end, ok := closing[head-1]; ok {
			ranges = append(ranges, [2]int{head, end})
		}

		from = head
	}

	for from := 0; ; {
		start := strings.Index(input[from:], "http")
		if start < 0 {
			break
		}

		start += from

		end := literalURLMatchEnd(input, start)
		if end < 0 {
			from = start + 1

			continue
		}

		ranges = append(ranges, [2]int{start, end})
		from = end
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

func literalURLMatchEnd[T ~string | ~[]byte](input T, start int) int {
	var tail int

	switch {
	case hasASCIIPrefix(input[start:], "https://"):
		tail = start + len("https://")
	case hasASCIIPrefix(input[start:], "http://"):
		tail = start + len("http://")
	default:
		return -1
	}

	end := tail
	for end < len(input) && !literalURLStop(input[end]) {
		end++
	}

	if end == tail {
		return -1
	}

	return end
}

func hasASCIIPrefix[T ~string | ~[]byte](input T, prefix string) bool {
	if len(input) < len(prefix) {
		return false
	}

	for i := range len(prefix) {
		if input[i] != prefix[i] {
			return false
		}
	}

	return true
}

func literalURLStop(char byte) bool {
	switch char {
	case '\t', '\n', '\f', '\r', ' ', '<', '>':
		return true
	default:
		return false
	}
}

func referenceHeadEnd(input string, from int) int {
	for {
		open := strings.IndexByte(input[from:], '[')
		if open < 0 {
			return -1
		}

		open += from

		end := strings.IndexByte(input[open+1:], ']')
		if end < 0 {
			return -1
		}

		end += open + 1
		if end+1 < len(input) && input[end+1] == '(' {
			return end + 2
		}

		from = end + 1
	}
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
