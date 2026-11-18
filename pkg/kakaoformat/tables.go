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
	line = strings.ReplaceAll(line, `\|`, "")

	return strings.Count(line, "|") + 1
}
