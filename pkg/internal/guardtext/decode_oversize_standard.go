package guardtext

import (
	"html"
	"slices"
	"unicode/utf8"
)

const oversizedTransformContextRunes = 256

func isWholeContextTransform(kind decodeFamily) bool {
	switch kind {
	case decodePercent, decodeHTML, decodeJSON:
		return true
	case decodeBase64, decodeHex:
		return false
	default:
		return false
	}
}

func boundedStandardTransformCandidates(
	input string,
	kind decodeFamily,
	spans []encodedSpan,
	mayContribute func(string) bool,
) ([]string, DecodeStatus) {
	windows := standardTransformWindows(input, spans)
	candidates := make([]string, 0, min(len(windows), maxDecodeCandidates))

	for _, windowSpan := range windows {
		window := input[windowSpan.start:windowSpan.end]
		decoded, ok := decodeWholeContextTransform(kind, window)

		if !ok || decoded == window || slices.Contains(candidates, decoded) {
			continue
		}

		if len(decoded) > maxDecodedCandidateLen {
			return candidates, DecodeByteLimit
		}

		contributes, status := protectedDecodedContribution(decoded, mayContribute)
		if status != 0 {
			return candidates, status
		}

		if !contributes {
			continue
		}

		candidates = append(candidates, decoded)
		if len(candidates) > maxDecodeCandidates {
			return candidates[:maxDecodeCandidates], DecodeCandidateLimit
		}
	}

	return candidates, 0
}

func standardTransformWindows(input string, spans []encodedSpan) []encodedSpan {
	windows := make([]encodedSpan, 0, len(spans))
	for _, span := range spans {
		window := encodedSpan{
			start: moveRuneStart(input, span.start, oversizedTransformContextRunes),
			end:   moveRuneEnd(input, span.end, oversizedTransformContextRunes),
		}
		last := len(windows) - 1

		if last >= 0 && window.start <= windows[last].end {
			windows[last].end = max(windows[last].end, window.end)

			continue
		}

		windows = append(windows, window)
	}

	return windows
}

func moveRuneStart(input string, end, count int) int {
	start := end

	for range count {
		if start <= 0 {
			return 0
		}

		_, size := utf8.DecodeLastRuneInString(input[:start])
		if size == 0 {
			return start
		}

		start -= size
	}

	return start
}

func moveRuneEnd(input string, start, count int) int {
	end := start

	for range count {
		if end >= len(input) {
			return len(input)
		}

		_, size := utf8.DecodeRuneInString(input[end:])
		if size == 0 {
			return end
		}

		end += size
	}

	return end
}

func decodeWholeContextTransform(kind decodeFamily, input string) (string, bool) {
	switch kind {
	case decodePercent:
		return decodePercentRuns(input)
	case decodeHTML:
		decoded := html.UnescapeString(input)

		return decoded, decoded != input
	case decodeJSON:
		return decodeJSONStringEscapes(input)
	case decodeBase64, decodeHex:
		return "", false
	default:
		return "", false
	}
}
