package guardtext

import (
	"strings"
	"unicode/utf8"
)

// writeNormalizedASCII는 ASCII 글자별 대응표로 바꾼 text를 이어 쓰고, 끝에 쓰지 않고 접어 둔 공백이 남았는지 돌려준다.
// 앞에서 접어 둔 공백은 pendingSpace로 받는다.
func writeNormalizedASCII(normalized *strings.Builder, text string, trimEdges, pendingSpace bool) bool {
	for i := range len(text) {
		replacement := normalizeASCIIReplacement[text[i]]

		if replacement == "" {
			continue
		}

		if replacement == " " {
			pendingSpace = normalized.Len() > 0 || !trimEdges
			continue
		}

		if pendingSpace {
			normalized.WriteByte(' ')

			pendingSpace = false
		}

		normalized.WriteString(replacement)
	}

	return pendingSpace
}

// normalizeFastHangulASCIIText는 ASCII와 한글 음절만 있는 글을 전체 경로와 같은 값으로 정규화한다. 이런 글은 NFKC에서
// 바뀌지 않고, normalizeWithKoreanPreserved는 한글 음절을 그대로 둔 채 그 사이 ASCII 구간마다 위장 I를 접고 정규화한다.
// ASCII 구간은 ASCII 빠른 경로와 같은 글자별 대응표로 바꾸고, 공백은 후처리처럼 글 전체에서 접는다. 한글 음절은
// 공백·제어 문자가 아니고 소문자화로 바뀌지 않는다.
func normalizeFastHangulASCIIText(text string, trimEdges bool) (string, bool) {
	for _, value := range text {
		if value >= utf8.RuneSelf && (value < 0xAC00 || value > 0xD7A3) {
			return "", false
		}
	}

	var normalized strings.Builder

	normalized.Grow(len(text))

	pendingSpace := false

	for rest := text; rest != ""; {
		ascii := len(rest)
		if index := strings.IndexFunc(rest, func(value rune) bool { return value >= utf8.RuneSelf }); index >= 0 {
			ascii = index
		}

		pendingSpace = writeNormalizedASCII(&normalized, foldDisguisedCapitalI(rest[:ascii]), trimEdges, pendingSpace)
		rest = rest[ascii:]

		for rest != "" && rest[0] >= utf8.RuneSelf {
			if pendingSpace {
				normalized.WriteByte(' ')

				pendingSpace = false
			}

			_, size := utf8.DecodeRuneInString(rest)
			normalized.WriteString(rest[:size])

			rest = rest[size:]
		}
	}

	if pendingSpace && !trimEdges {
		normalized.WriteByte(' ')
	}

	return normalized.String(), true
}
