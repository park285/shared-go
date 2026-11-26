package kakaoformat

import (
	"strconv"
	"strings"
)

type store struct {
	kind   string
	values []string
}

func newStore(kind string) *store {
	return &store{kind: kind}
}

func (s *store) Put(value string) string {
	token := s.token(len(s.values))

	s.values = append(s.values, value)

	return token
}

func (s *store) Restore(text string) string {
	if len(s.values) == 0 {
		return text
	}

	var output strings.Builder

	last := 0

	for next := 0; ; {
		offset := strings.IndexByte(text[next:], '\x00')
		if offset < 0 {
			break
		}

		start := next + offset

		index, end, ok := s.match(text, start)
		if !ok {
			next = start + 1

			continue
		}

		if last == 0 {
			output.Grow(len(text))
		}

		output.WriteString(text[last:start])
		output.WriteString(s.values[index])

		last = end
		next = end
	}

	if last == 0 {
		return text
	}

	output.WriteString(text[last:])

	return output.String()
}

func (s *store) indentFollowingLines(text, prefix string) {
	for next := 0; ; {
		offset := strings.IndexByte(text[next:], '\x00')
		if offset < 0 {
			return
		}

		start := next + offset

		index, end, ok := s.match(text, start)
		if !ok {
			next = start + 1

			continue
		}

		lines := strings.Split(strings.TrimSuffix(s.values[index], "\n"), "\n")
		for line := 1; line < len(lines); line++ {
			if lines[line] != "" {
				lines[line] = prefix + lines[line]
			}
		}

		s.values[index] = strings.Join(lines, "\n")
		next = end
	}
}

func (s *store) match(text string, start int) (int, int, bool) {
	digits, ok := strings.CutPrefix(text[start+1:], s.kind)
	if !ok {
		return 0, 0, false
	}

	index, width := 0, 0
	for width < len(digits) && '0' <= digits[width] && digits[width] <= '9' {
		index = index*10 + int(digits[width]-'0')
		width++

		if index >= len(s.values) {
			return 0, 0, false
		}
	}

	if width == 0 || width == len(digits) || digits[width] != '\x00' || (width > 1 && digits[0] == '0') {
		return 0, 0, false
	}

	return index, start + 1 + len(s.kind) + width + 1, true
}

func (s *store) token(i int) string {
	return "\x00" + s.kind + strconv.Itoa(i) + "\x00"
}
