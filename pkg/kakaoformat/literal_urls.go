package kakaoformat

import "strings"

func insideCode(position int, spans [][]int, index *int) bool {
	for *index < len(spans) && spans[*index][1] <= position {
		*index++
	}

	return *index < len(spans) && spans[*index][0] <= position
}

func emphasisSpans(input string) [][][2]int {
	spans := make([][][2]int, 0, 7)

	for _, delimiter := range []string{"***", "___", "**", "__", "*", "_", "~~"} {
		var pairs [][2]int

		for offset := 0; offset < len(input); {
			start := findDelim(input, delimiter, offset, true)
			if start < 0 {
				break
			}

			end := findDelim(input, delimiter, start+len(delimiter), false)
			if end < 0 {
				break
			}

			pairs = append(pairs, [2]int{start + len(delimiter), end})
			offset = end + len(delimiter)
		}

		spans = append(spans, pairs)
	}

	return spans
}

func protectLiteralURLs(input string, urls *store) string {
	code := reInlineCode.FindAllStringIndex(input, -1)
	codeIndex := 0
	spans := emphasisSpans(input)
	indices := make([]int, len(spans))

	var output strings.Builder

	last := 0

	for _, match := range reLiteralURL.FindAllStringIndex(input, -1) {
		start, end := match[0], match[1]
		if insideCode(start, code, &codeIndex) {
			continue
		}

		for kind, pairs := range spans {
			for indices[kind] < len(pairs) && pairs[indices[kind]][1] <= start {
				indices[kind]++
			}

			if indices[kind] < len(pairs) {
				pair := pairs[indices[kind]]
				if pair[0] <= start && pair[1] < end {
					end = pair[1]
				}
			}
		}

		output.WriteString(input[last:start])
		output.WriteString(urls.Put(input[start:end]))

		last = end
	}

	output.WriteString(input[last:])

	return output.String()
}
