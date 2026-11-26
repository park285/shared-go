package kakaoformat

import (
	"strings"
)

func escapeDateListMarkers(input string) string {
	var output strings.Builder

	last := 0

	for start := 0; ; {
		if dot := dateListYearDot(input, start); dot >= 0 {
			if last == 0 {
				output.Grow(len(input) + 8)
			}

			output.WriteString(input[last:dot])
			output.WriteByte('\\')

			last = dot
		}

		next := strings.IndexByte(input[start:], '\n')
		if next < 0 {
			break
		}

		start += next + 1
	}

	if last == 0 {
		return input
	}

	output.WriteString(input[last:])

	return output.String()
}

func dateListYearDot(input string, start int) int {
	year := skipBlanks(input, start, 3)

	dot := skipDigits(input, year, 4)
	if dot-year < 4 || dot >= len(input) || input[dot] != '.' {
		return -1
	}

	month := skipBlanks(input, dot+1, len(input))
	if month == dot+1 {
		return -1
	}

	end := skipDigits(input, month, 2)
	if end == month || end >= len(input) || input[end] != '.' {
		return -1
	}

	return dot
}

func skipBlanks(input string, i, limit int) int {
	end := i
	for end < len(input) && end-i < limit && (input[end] == ' ' || input[end] == '\t') {
		end++
	}

	return end
}

func skipDigits(input string, i, limit int) int {
	end := i
	for end < len(input) && end-i < limit && asciiDigit(rune(input[end])) {
		end++
	}

	return end
}

func listIndent(level int) string {
	return strings.Repeat("  ", level)
}
