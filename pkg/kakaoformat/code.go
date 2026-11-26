package kakaoformat

import "strings"

func protectCodeRanges(input string, dst *store) (string, [][2]int) {
	var (
		output     strings.Builder
		containers [][2]int
	)

	output.Grow(len(input))

	last := 0
	destinations := literalDestinations(input)
	destination := 0

	for _, code := range CodeRanges(input) {
		if code.Container {
			offset := output.Len() + code.Start - last

			containers = append(containers, [2]int{offset, offset + code.End - code.Start})

			continue
		}

		for destination < len(destinations) && destinations[destination][1] <= code.Start {
			destination++
		}

		if !code.Block && destination < len(destinations) && destinations[destination][0] < code.Start && code.End <= destinations[destination][1] {
			continue
		}

		output.WriteString(input[last:code.Start])

		value := formatCodeRange(input, code)

		output.WriteString(dst.Put(value))

		if strings.HasSuffix(input[code.Start:code.End], "\n") {
			output.WriteByte('\n')
		}

		last = code.End
	}

	output.WriteString(input[last:])

	return output.String(), containers
}

func formatCodeRange(input string, code CodeRange) string {
	if !code.Block {
		return input[code.BodyStart:code.BodyEnd]
	}

	if !code.Fenced {
		return input[code.Start:code.End]
	}

	return dedentLines(input[code.BodyStart:code.BodyEnd], len(code.Indent))
}

func dedentLines(body string, width int) string {
	if width == 0 {
		return body
	}

	lines := strings.Split(body, "\n")
	for index, line := range lines {
		prefix := line[:min(width, len(line))]

		lines[index] = line[len(prefix)-len(strings.TrimLeft(prefix, " ")):]
	}

	return strings.Join(lines, "\n")
}
