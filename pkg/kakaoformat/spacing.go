package kakaoformat

import (
	"strings"
)

// cleanupSpacing은 줄 앞 공백·탭은 두고 줄 끝 공백·탭만 지운 뒤 연속 개행 3개 이상을 2개로 줄인다.
// 공백·탭만 있는 줄은 그대로 남으므로 빈 줄이 되지 않고 개행 연속을 끊는다. 처리 뒤 비는 줄은 원래 빈 줄뿐이므로
// 개행을 미뤄 세었다가 다음 내용 앞이나 끝에서 한 번에 쓰면 줄을 모두 이은 뒤 개행 연속을 줄이는 것과 같다.
func cleanupSpacing(input string) string {
	var output strings.Builder

	// 지우기만 하므로 결과는 입력보다 길지 않다.
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

// writeNewlines는 개행 count개를 쓰되 3개 이상이면 2개만 쓴다.
func writeNewlines(output *strings.Builder, count int) {
	for range min(count, 2) {
		output.WriteByte('\n')
	}
}
