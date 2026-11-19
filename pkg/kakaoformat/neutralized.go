package kakaoformat

import "strings"

const neutralizedMarkers = "*_`~]#&<\\"

func protectNeutralizedMarkers(input string, dst *store) string {
	if !strings.Contains(input, "\u200b") {
		return input
	}

	var (
		output strings.Builder
		tokens [len(neutralizedMarkers)]string
	)

	start := 0

	for i := 0; i < len(input); i++ {
		marker := strings.IndexByte(neutralizedMarkers, input[i])
		if marker < 0 || !strings.HasPrefix(input[i+1:], "\u200b") {
			continue
		}

		end := i + 1 + len("\u200b")

		if tokens[marker] == "" {
			tokens[marker] = dst.Put(input[i:end])
		}

		if start == 0 {
			output.Grow(len(input))
		}

		output.WriteString(input[start:i])
		output.WriteString(tokens[marker])

		start = end
		i = end - 1
	}

	if start == 0 {
		return input
	}

	output.WriteString(input[start:])

	return output.String()
}
