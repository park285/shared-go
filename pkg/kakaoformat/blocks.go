package kakaoformat

import (
	"strings"
)

// escapeDateListMarkers는 줄 첫머리의 `2026. 10. 4.` 날짜가 중첩 번호 목록으로 해석되지 않도록 연도 점을 이스케이프한다.
// `(?m)^([ \t]{0,3})(\d{4})\.([ \t]+\d{1,2}\.)`를 `$1$2\.$3`로 바꾸는 것과 같다. 줄 시작은 입력 처음과 '\n' 뒤뿐이며 '\r' 뒤는 아니다.
func escapeDateListMarkers(input string) string {
	var output strings.Builder

	// 연도 점은 줄 시작에서 최소 4바이트 뒤에 있으므로 last가 0이면 아직 바꾼 줄이 없다.
	last := 0

	for start := 0; ; {
		if dot := dateListYearDot(input, start); dot >= 0 {
			if last == 0 {
				// 바꿀 줄이 있을 때만 결과를 만든다. 바꾼 줄마다 역슬래시 하나가 늘어난다.
				output.Grow(len(input) + 8)
			}

			output.WriteString(input[last:dot])
			output.WriteByte('\\')

			last = dot
		}

		next := strings.IndexByte(input[start:], '\n')
		if next < 0 {
			break
		}

		start += next + 1
	}

	if last == 0 {
		return input
	}

	output.WriteString(input[last:])

	return output.String()
}

// dateListYearDot은 start에서 시작하는 줄이 `[ \t]{0,3}\d{4}\.[ \t]+\d{1,2}\.`로 시작하면 연도 뒤 '.'의 위치를, 아니면 -1을 돌려준다.
// 공백·탭과 숫자와 '.'은 서로 겹치지 않으므로 각 반복을 탐욕적으로 한 번 훑은 결과가 정규식의 되추적 결과와 같다.
func dateListYearDot(input string, start int) int {
	year := skipBlanks(input, start, 3)

	dot := skipDigits(input, year, 4)
	if dot-year < 4 || dot >= len(input) || input[dot] != '.' {
		return -1
	}

	month := skipBlanks(input, dot+1, len(input))
	if month == dot+1 {
		return -1
	}

	end := skipDigits(input, month, 2)
	if end == month || end >= len(input) || input[end] != '.' {
		return -1
	}

	return dot
}

// skipBlanks는 input[i:]의 공백·탭을 최대 limit개 건너뛴 위치를 돌려준다.
func skipBlanks(input string, i, limit int) int {
	end := i
	for end < len(input) && end-i < limit && (input[end] == ' ' || input[end] == '\t') {
		end++
	}

	return end
}

// skipDigits는 input[i:]의 ASCII 숫자를 최대 limit개 건너뛴 위치를 돌려준다.
func skipDigits(input string, i, limit int) int {
	end := i
	for end < len(input) && end-i < limit && asciiDigit(rune(input[end])) {
		end++
	}

	return end
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
