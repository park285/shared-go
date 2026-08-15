package kakaoformat

import (
	"regexp"
	"strings"
)

var reBlankRun = regexp.MustCompile(`\n{3,}`)

func cleanupSpacing(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = lead + strings.TrimRight(line[len(lead):], " \t")
	}

	return reBlankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}
