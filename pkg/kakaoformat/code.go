package kakaoformat

import "strings"

func protectCodeRanges(input string, dst *store) string {
	var output strings.Builder

	output.Grow(len(input))

	last := 0
	destinations := literalDestinations(input)
	destination := 0

	for _, code := range CodeRanges(input) {
		if code.Container {
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

	return output.String()
}

func formatCodeRange(input string, code CodeRange) string {
	if !code.Block {
		return input[code.BodyStart:code.BodyEnd]
	}

	if !code.Fenced {
		return input[code.Start:code.End]
	}

	return input[code.BodyStart:code.BodyEnd]
}
