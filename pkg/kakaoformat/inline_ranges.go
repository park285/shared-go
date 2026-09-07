package kakaoformat

import "strings"

func inlineCodeRanges(input string, offset int) []CodeRange {
	type run struct{ start, end int }

	var runs []run

	for i := 0; i < len(input); {
		if input[i] != '`' {
			i++
			continue
		}

		end := i + 1
		for end < len(input) && input[end] == '`' {
			end++
		}

		runs = append(runs, run{i, end})
		i = end
	}

	closing := make(map[int]int, len(runs))
	next := make(map[int]int)

	for i := len(runs) - 1; i >= 0; i-- {
		if i+1 < len(runs) {
			gap := input[runs[i].end:runs[i+1].start]
			if strings.Contains(gap, "\n\n") || strings.ContainsRune(gap, 0) {
				clear(next)
			}
		}

		width := runs[i].end - runs[i].start
		if end, ok := next[width]; ok {
			closing[i] = end
		}

		next[width] = i
	}

	var result []CodeRange

	for i := 0; i < len(runs); i++ {
		end, ok := closing[i]
		if !ok || markdownEscaped(input, runs[i].start) {
			continue
		}

		result = append(result, CodeRange{Start: offset + runs[i].start, End: offset + runs[end].end, BodyStart: offset + runs[i].end, BodyEnd: offset + runs[end].start, Width: runs[i].end - runs[i].start})
		i = end
	}

	return result
}
