package kakaoformat

import (
	"strconv"
	"strings"
)

const (
	maxTableColumns     = 32
	maxTableRows        = 200
	maxTableOutputLines = 4000
)

func renderTables(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	wroteTable := false

	for i := 0; i < len(lines); {
		if !looksLikeTable(lines, i) {
			out = append(out, lines[i])
			i++
			continue
		}

		headers := parseTableRow(lines[i])
		i += 2

		rows := make([][]string, 0, 4)
		for i < len(lines) && strings.Count(lines[i], "|") >= 2 {
			if len(rows) < maxTableRows {
				rows = append(rows, parseTableRow(lines[i]))
			}
			i++
		}

		for hi, header := range headers {
			if hi >= maxTableColumns || len(out) >= maxTableOutputLines {
				break
			}
			header = strings.TrimSpace(header)
			if header == "" {
				continue
			}

			out = append(out, "【"+header+"】")
			n := 0
			for _, row := range rows {
				if len(out) >= maxTableOutputLines {
					break
				}
				value := ""
				if hi < len(row) {
					value = strings.TrimSpace(row[hi])
				}
				n++
				out = append(out, "    《"+strconv.Itoa(n)+"》 "+value)
			}
			out = append(out, "-------------------------")
			wroteTable = true
		}
	}

	result := strings.Join(out, "\n")
	if wroteTable && strings.HasSuffix(result, "-------------------------") {
		return result + "\n"
	}

	return result
}

func looksLikeTable(lines []string, i int) bool {
	if i+1 >= len(lines) || strings.Count(lines[i], "|") < 2 {
		return false
	}

	sep := strings.TrimSpace(lines[i+1])
	if !strings.HasPrefix(sep, "|") || !strings.Contains(sep, "-") {
		return false
	}

	for _, r := range sep {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}

	return true
}

func parseTableRow(line string) []string {
	var (
		parts []string
		b     strings.Builder
		code  bool
	)

	for _, r := range line {
		switch r {
		case '`':
			code = !code
			b.WriteRune(r)
		case '|':
			if code {
				b.WriteRune(r)
				continue
			}
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	parts = append(parts, b.String())

	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}

	return parts
}
