package kakaoformat

import "strings"

func EscapeMarkdown(input string) string {
	var output strings.Builder

	lineBlank, digits := true, true

	for index, char := range input {
		if needsMarkdownEscape(input, index, char, lineBlank, digits) {
			output.WriteByte('\\')
		}

		output.WriteRune(char)

		if char == '\n' {
			lineBlank, digits = true, true
		} else if char != ' ' && char != '\t' {
			lineBlank = false
			digits = digits && char >= '0' && char <= '9'
		} else if !lineBlank {
			digits = false
		}
	}

	return output.String()
}

func needsMarkdownEscape(input string, index int, char rune, lineBlank, digits bool) bool {
	if strings.ContainsRune("\\`*_[]|~#&", char) {
		return true
	}

	if lineBlank && strings.ContainsRune(">-+=", char) {
		return true
	}

	if char == '<' && index+1 < len(input) {
		return input[index+1] != ' '
	}

	return (char == '.' || char == ')') && !lineBlank && digits &&
		(index+1 == len(input) || input[index+1] == ' ' || input[index+1] == '\t')
}

func TransformEscapes(input string, transform func(string) string) string {
	var output strings.Builder

	for i := 0; i < len(input); i++ {
		if input[i] == '\\' && i+1 < len(input) && markdownPunctuation(input[i+1]) {
			output.WriteString(transform(input[i+1 : i+2]))

			i++
		} else {
			output.WriteByte(input[i])
		}
	}

	return output.String()
}

func markdownPunctuation(char byte) bool {
	return char >= '!' && char <= '/' || char >= ':' && char <= '@' || char >= '[' && char <= '`' || char >= '{' && char <= '~'
}
