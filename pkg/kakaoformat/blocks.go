package kakaoformat

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reHeading   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reChecklist = regexp.MustCompile(`^([ \t]*)([-*])\s+\[([ xX])\]\s+(.*)$`)
	reBullet    = regexp.MustCompile(`^([ \t]*)([-*+])\s+(.*)$`)
	reOrdered   = regexp.MustCompile(`^([ \t]*)(\d+)\.\s+(.*)$`)
	reQuote     = regexp.MustCompile(`^((?:>\s*)+)(.*)$`)
)

func renderLines(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lines[i] = renderLine(line)
	}

	return strings.Join(lines, "\n")
}

func renderLine(line string) string {
	if isHorizontalRule(line) {
		return strings.Repeat("━", 20)
	}

	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return line
	}

	switch trimmed[0] {
	case '#':
		if parts := reHeading.FindStringSubmatch(line); len(parts) == 3 {
			return "\n【" + strings.TrimSpace(parts[2]) + "】\n"
		}
	case '>':
		if parts := reQuote.FindStringSubmatch(line); len(parts) == 3 {
			depth := strings.Count(parts[1], ">")

			return strings.Repeat("  ", depth) + strings.Repeat("‖ ", depth) + strings.TrimSpace(parts[2])
		}
	case '-', '*', '+', '_':
		if parts := reChecklist.FindStringSubmatch(line); len(parts) == 5 {
			mark := "✖"
			if strings.EqualFold(strings.TrimSpace(parts[3]), "x") {
				mark = "✔"
			}

			return listIndent(len(parts[1])/2) + mark + " " + parts[4]
		}
		if parts := reBullet.FindStringSubmatch(line); len(parts) == 4 {
			level := len(parts[1]) / 2

			return listIndent(level) + bulletFor(level) + " " + parts[3]
		}
	default:
		if trimmed[0] >= '0' && trimmed[0] <= '9' {
			if parts := reOrdered.FindStringSubmatch(line); len(parts) == 4 {
				return listIndent(len(parts[1])/2) + parts[2] + ". " + parts[3]
			}
		}
	}

	return line
}

func listIndent(level int) string {
	return strings.Repeat("  ", level)
}

func bulletFor(level int) string {
	switch {
	case level <= 0:
		return "⦁"
	case level == 1:
		return "￮"
	case level == 2:
		return "▸"
	default:
		return "▹"
	}
}

func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}

	marker := trimmed[0]
	if marker != '-' && marker != '*' && marker != '_' {
		return false
	}

	count := 0
	for _, r := range trimmed {
		if r == rune(marker) {
			count++
			continue
		}
		if !unicode.IsSpace(r) {
			return false
		}
	}

	return count >= 3
}
