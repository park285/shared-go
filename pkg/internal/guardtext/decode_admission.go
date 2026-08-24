package guardtext

import (
	"strings"
	"unicode/utf8"
)

const (
	admissionContextUnits    = 256
	admissionSeparatorKeep   = 8
	admissionRawBytesPerSide = maxDecodedCandidateLen
)

// contextualAdmissionCandidate는 승격용 경계 윈도우 splice를 만든다. 구분자 run은
// projection(JoinShortSeparators)에서 길이와 무관하게 접히므로 raw 거리로 자르면
// run 너머와의 재조합 탐지가 끊긴다: run은 정보 단위 1로 세어 윈도우를 그 너머까지
// 연장하고, 승격 문자열에는 run을 admissionSeparatorKeep rune으로 절단해 넣는다
// (절단 전후 projection이 같아 exact 매칭이 동일하다). Raw 한도를 넘는 연장은
// 기존 계약과 같이 미완료(fail-closed)로 보고한다.
func contextualAdmissionCandidate(input string, span encodedSpan, decoded string) (string, bool) {
	prefix := input[:span.start]
	suffix := input[span.end:]
	prefixStart, prefixOK := admissionWindowStart(prefix)
	suffixEnd, suffixOK := admissionWindowEnd(suffix)

	if !prefixOK || !suffixOK {
		return "", false
	}

	var contextual strings.Builder

	contextual.Grow(len(prefix) - prefixStart + len(decoded) + suffixEnd)
	writeCollapsedSeparatorRuns(&contextual, prefix[prefixStart:], true)
	contextual.WriteString(decoded)
	writeCollapsedSeparatorRuns(&contextual, suffix[:suffixEnd], false)

	return contextual.String(), true
}

func admissionWindowStart(text string) (int, bool) {
	units := 0
	position := len(text)

	for position > 0 && units < admissionContextUnits {
		value, size := utf8.DecodeLastRuneInString(text[:position])
		if !isJoinSeparator(value) {
			position -= size
			units++

			continue
		}

		for position > 0 {
			value, size = utf8.DecodeLastRuneInString(text[:position])
			if !isJoinSeparator(value) {
				break
			}

			position -= size
		}

		units++
	}

	if len(text)-position > admissionRawBytesPerSide {
		return 0, false
	}

	return position, true
}

func admissionWindowEnd(text string) (int, bool) {
	units := 0
	position := 0

	for position < len(text) && units < admissionContextUnits {
		value, size := utf8.DecodeRuneInString(text[position:])
		if !isJoinSeparator(value) {
			position += size
			units++

			continue
		}

		for position < len(text) {
			value, size = utf8.DecodeRuneInString(text[position:])
			if !isJoinSeparator(value) {
				break
			}

			position += size
		}

		units++
	}

	if position > admissionRawBytesPerSide {
		return 0, false
	}

	return position, true
}

func writeCollapsedSeparatorRuns(builder *strings.Builder, window string, keepRunTail bool) {
	for position := 0; position < len(window); {
		value, size := utf8.DecodeRuneInString(window[position:])
		if !isJoinSeparator(value) {
			builder.WriteRune(value)

			position += size

			continue
		}

		runStart := position //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		runRunes := 0
		keepHeadEnd := position   //nolint:copyloopvar // 이 사본은 아래에서 변형되므로 루프 변수를 그대로 쓸 수 없다.
		keepTailStart := position //nolint:copyloopvar // 이 사본은 아래에서 변형되므로 루프 변수를 그대로 쓸 수 없다.

		for position < len(window) {
			value, size = utf8.DecodeRuneInString(window[position:])
			if !isJoinSeparator(value) {
				break
			}

			position += size
			runRunes++

			if runRunes <= admissionSeparatorKeep {
				keepHeadEnd = position
			} else {
				_, skipped := utf8.DecodeRuneInString(window[keepTailStart:])

				keepTailStart += skipped
			}
		}

		switch {
		case runRunes <= admissionSeparatorKeep:
			builder.WriteString(window[runStart:position])
		case keepRunTail:
			builder.WriteString(window[keepTailStart:position])
		default:
			builder.WriteString(window[runStart:keepHeadEnd])
		}
	}
}
