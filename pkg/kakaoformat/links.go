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

		// 링크 머리가 하나도 없으면 괄호 짝을 계산하지 않는다.
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

// literalURLMatchEnd는 input[start:]가 `https?://[^\s<>]+`로 시작하면 그 끝 위치를, 아니면 -1을 돌려준다.
// 탐욕적인 `s?` 때문에 "https://"를 먼저 보며, 그 경우 start+4가 's'라 "http://"는 맞을 수 없다.
// 꼬리에서 빠지는 \s(\t \n \f \r 공백)와 '<' '>'는 모두 ASCII이고 정규식은 잘못된 UTF-8 바이트도 꼬리로 받으므로
// 바이트 단위로 끝을 찾아도 같다. "http"를 찾아 시작마다 이 함수를 부르고, 실패하면 다음 바이트에서, 맞으면 끝에서
// 다시 찾으면 FindAll의 결과와 같다.
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

// referenceHeadEnd는 input[from:]에서 `!?\[([^\]]*)\]\(`가 처음 끝나는 위치('(' 다음)를, 없으면 -1을 돌려준다.
// `[` 다음의 첫 `]`만 닫는 괄호가 될 수 있으므로, 그 뒤가 `(`가 아니면 그 사이의 다른 `[`도 모두 실패하고
// 다음 후보는 그 `]` 뒤에서 찾는다. `[^\]]`는 개행도 포함하므로 개행에서 멈추지 않는다.
// 앞의 `!`는 끝 위치를 바꾸지 않는다. 끝 위치에서 다시 찾으면 정규식 FindAll의 겹치지 않는 결과와 같다.
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
