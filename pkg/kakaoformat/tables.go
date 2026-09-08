package kakaoformat

import (
	"strings"
)

const (
	maxTableColumns     = 32
	maxTableRows        = 200
	maxTableOutputLines = 4000
)

func tableColumnCount(line string) int {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	// 유효한 code span은 이미 보호됐으므로 남은 백틱은 구분 파이프를 숨기지 않는다.
	line = strings.ReplaceAll(line, `\|`, "")

	return strings.Count(line, "|") + 1
}
