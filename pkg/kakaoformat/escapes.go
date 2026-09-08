package kakaoformat

import "strings"

// EscapeMarkdown은 평문이 Markdown 구문으로 해석되지 않도록 문장 부호를 이스케이프한다.
// 코드 구간 밖에 삽입하는 표시용 텍스트에 적용하며 원문은 변경하지 않는다.
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

// TransformEscapes는 Markdown 이스케이프 한 개마다 해제한 문자를 transform에 전달한다.
// 호출자는 코드·주소를 먼저 보호하고 반환값으로 후속 서식 처리에서 문자를 보호할 수 있다.
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
