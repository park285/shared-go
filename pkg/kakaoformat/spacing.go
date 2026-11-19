package kakaoformat

import (
	"strings"
)

func cleanupSpacing(input string) string {
	var output strings.Builder

	output.Grow(len(input))

	newlines := 0

	for rest := input; ; {
		line, tail, more := strings.Cut(rest, "\n")
		if strings.TrimLeft(line, " \t") != "" {
			line = strings.TrimRight(line, " \t")
		}

		if line != "" {
			writeNewlines(&output, newlines)
			output.WriteString(line)

			newlines = 0
		}

		if !more {
			break
		}

		rest = tail
		newlines++
	}

	writeNewlines(&output, newlines)

	return output.String()
}

func writeNewlines(output *strings.Builder, count int) {
	for range min(count, 2) {
		output.WriteByte('\n')
	}
}
