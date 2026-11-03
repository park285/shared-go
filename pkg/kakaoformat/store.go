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

// 왼쪽부터 겹치지 않게 이 store의 완전한 보호 표식만 바꾸고, 바꾼 값은 다시 훑지 않는다.
// 표식은 모두 NUL로 시작하므로 NUL 위치만 후보로 보면 된다. 잘리거나 다른 store의 표식은 원문대로 두고
// 그 다음 바이트부터 다시 찾아, 어긋난 NUL 뒤에 붙은 완전한 표식도 놓치지 않는다.
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

		value, end, ok := s.match(text, start)
		if !ok {
			next = start + 1

			continue
		}

		if last == 0 {
			// 첫 표식을 찾았을 때만 결과를 만든다. 표식과 값의 길이 차이는 알 수 없으므로 입력 크기를 기준으로 잡는다.
			output.Grow(len(text))
		}

		output.WriteString(text[last:start])
		output.WriteString(value)

		last = end
		next = end
	}

	if last == 0 {
		return text
	}

	output.WriteString(text[last:])

	return output.String()
}

// text[start]의 NUL에서 token이 만드는 표식과 정확히 같은 바이트열이 시작하면 값과 표식 끝 위치를 돌려준다.
// 번호는 strconv.Itoa 결과와 같도록 앞자리 0을 "0" 하나만 인정하고, 범위를 넘는 즉시 멈추므로 넘침이 없다.
func (s *store) match(text string, start int) (string, int, bool) {
	digits, ok := strings.CutPrefix(text[start+1:], s.kind)
	if !ok {
		return "", 0, false
	}

	index, width := 0, 0
	for width < len(digits) && '0' <= digits[width] && digits[width] <= '9' {
		index = index*10 + int(digits[width]-'0')
		width++

		if index >= len(s.values) {
			return "", 0, false
		}
	}

	if width == 0 || width == len(digits) || digits[width] != '\x00' || (width > 1 && digits[0] == '0') {
		return "", 0, false
	}

	return s.values[index], start + 1 + len(s.kind) + width + 1, true
}

func (s *store) token(i int) string {
	return "\x00" + s.kind + strconv.Itoa(i) + "\x00"
}

// 강조·취소선은 보호 표식 내부의 문자를 변환하지 않는다.
func mapOutsidePlaceholders(text string, transform func(string) string) string {
	var output strings.Builder

	// 변환은 문자 단위 치환이라 결과가 입력과 비슷한 크기이므로 재할당을 줄이도록 미리 잡는다.
	output.Grow(len(text))

	for {
		start := strings.IndexByte(text, '\x00')
		if start < 0 {
			output.WriteString(transform(text))

			break
		}

		end := strings.IndexByte(text[start+1:], '\x00')
		if end < 0 {
			output.WriteString(transform(text))

			break
		}

		end += start + 2
		output.WriteString(transform(text[:start]))
		output.WriteString(text[start:end])

		text = text[end:]
	}

	return output.String()
}
