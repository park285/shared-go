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

func contextualAdmissionCandidate(input string, span encodedSpan, decoded string) (string, bool) {
	contextual, _, bounded := contextualAdmissionDetails(input, span, decoded)
	return contextual, bounded
}

func contextualAdmissionDetails(input string, span encodedSpan, decoded string) (string, encodedSpan, bool) {
	prefix := input[:span.start]
	suffix := input[span.end:]
	prefixStart, prefixOK := admissionWindowStart(prefix)
	suffixEnd, suffixOK := admissionWindowEnd(suffix)

	if !prefixOK || !suffixOK {
		return "", encodedSpan{}, false
	}

	var contextual strings.Builder

	contextual.Grow(len(prefix) - prefixStart + len(decoded) + suffixEnd)
	writeCollapsedSeparatorRuns(&contextual, prefix[prefixStart:], true)

	replacement := encodedSpan{start: contextual.Len()}
	contextual.WriteString(decoded)
	writeCollapsedSeparatorRuns(&contextual, suffix[:suffixEnd], false)

	replacement.end = replacement.start + len(decoded)

	return contextual.String(), replacement, true
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
