package guardtext

import (
	"html"
	"strings"
)

func (d *contextDecoder) ruleCandidateMayExpand(source, candidate string) bool {
	if !consumeProtectedContextWork(&d.protectedWork, &d.result.Status, len(source)) ||
		!consumeProtectedContextWork(&d.protectedWork, &d.result.Status, len(candidate)) {
		return false
	}

	if introducesSurface, ok := wholeTransformIntroducesRuleSurface(source, candidate); ok {
		return introducesSurface
	}
	if changed, ok := transformedCandidateRange(source, candidate); ok {
		return ruleDecodeSurfaceOverlaps(candidate, changed)
	}

	return ruleDecodeSurfaceOverlaps(candidate, encodedSpan{end: len(candidate)})
}

func transformedCandidateRange(source, candidate string) (encodedSpan, bool) {
	if source == candidate {
		return encodedSpan{}, false
	}

	for position := 0; position < len(source); {
		start := position
		match := nextBase64Candidate(source, position)
		position = match.next
		if len(match.value) < 4 {
			continue
		}
		if replacement, ok := replacementRange(source, candidate, encodedSpan{start: start, end: match.next}); ok {
			return replacement, true
		}
	}
	if !containsASCIIFold(source, "hex") {
		return encodedSpan{}, false
	}
	for _, span := range hexSpansForPattern(source, hexPayloadPattern) {
		span.start = contextualHexStart(source, span.start)
		if replacement, ok := replacementRange(source, candidate, span); ok {
			return replacement, true
		}
	}
	for _, span := range shortRuleHexSpans(source) {
		span.start = contextualHexStart(source, span.start)
		if replacement, ok := replacementRange(source, candidate, span); ok {
			return replacement, true
		}
	}

	return encodedSpan{}, false
}

func replacementRange(source, candidate string, span encodedSpan) (encodedSpan, bool) {
	if span.start < 0 || span.start > span.end || span.end > len(source) {
		return encodedSpan{}, false
	}
	suffixBytes := len(source) - span.end
	candidateEnd := len(candidate) - suffixBytes
	if candidateEnd < span.start || len(candidate) < suffixBytes ||
		candidate[:span.start] != source[:span.start] || candidate[candidateEnd:] != source[span.end:] {
		return encodedSpan{}, false
	}

	return encodedSpan{start: span.start, end: candidateEnd}, true
}

func wholeTransformIntroducesRuleSurface(source, candidate string) (bool, bool) {
	if strings.IndexByte(source, '%') >= 0 {
		if decoded, ok := decodePercentRuns(source); ok && decoded == candidate {
			return transformedSpansIntroduceRuleSurface(source, candidate, percentSpans(source), decodePercentRuns), true
		}
	}
	if strings.IndexByte(source, '&') >= 0 {
		if decoded := html.UnescapeString(source); decoded != source && decoded == candidate {
			return transformedSpansIntroduceRuleSurface(source, candidate, htmlEntitySpans(source), decodeHTMLEntity), true
		}
	}
	if strings.IndexByte(source, '\\') >= 0 {
		if decoded, ok := decodeJSONStringEscapes(source); ok && decoded == candidate {
			return transformedSpansIntroduceRuleSurface(source, candidate, jsonEscapeSpans(source), decodeJSONStringEscapes), true
		}
	}

	return false, false
}

func transformedSpansIntroduceRuleSurface(
	source string,
	candidate string,
	spans []encodedSpan,
	decode func(string) (string, bool),
) bool {
	sourcePosition := 0
	candidatePosition := 0
	for _, span := range spans {
		candidatePosition += span.start - sourcePosition
		replacement, changed := decode(source[span.start:span.end])
		if !changed {
			replacement = source[span.start:span.end]
		}
		replacementSpan := encodedSpan{start: candidatePosition, end: candidatePosition + len(replacement)}
		if changed && ruleDecodeSurfaceOverlaps(candidate, replacementSpan) {
			return true
		}
		candidatePosition = replacementSpan.end
		sourcePosition = span.end
	}

	return false
}

func decodeHTMLEntity(input string) (string, bool) {
	decoded := html.UnescapeString(input)

	return decoded, decoded != input
}

func ruleDecodeSurfaceOverlaps(input string, changed encodedSpan) bool {
	if changed.start < 0 || changed.start >= changed.end || changed.end > len(input) {
		return false
	}

	return base64RuleSurfaceOverlaps(input, changed) ||
		escapeRuleSurfaceOverlaps(input, changed) ||
		hexRuleSurfaceOverlaps(input, changed)
}

func base64RuleSurfaceOverlaps(input string, changed encodedSpan) bool {
	for position := 0; position < len(input); {
		start := position
		match := nextBase64Candidate(input, position)
		position = match.next
		span := encodedSpan{start: start, end: match.next}
		if !encodedSpansOverlap(span, changed) || len(match.value) < 4 {
			continue
		}
		if len(match.value) >= minBase64CandidateLen || plausibleShortBase64Value(match.value) {
			return true
		}
	}

	return false
}

func escapeRuleSurfaceOverlaps(input string, changed encodedSpan) bool {
	if strings.IndexByte(input, '%') >= 0 {
		for _, span := range percentSpans(input) {
			if encodedSpansOverlap(span, changed) {
				return true
			}
		}
	}
	if strings.IndexByte(input, '&') >= 0 {
		for _, span := range htmlEntitySpans(input) {
			if encodedSpansOverlap(span, changed) {
				return true
			}
		}
	}
	if strings.IndexByte(input, '\\') >= 0 {
		for _, span := range jsonEscapeSpans(input) {
			if encodedSpansOverlap(span, changed) {
				return true
			}
		}

	}

	return false
}

func hexRuleSurfaceOverlaps(input string, changed encodedSpan) bool {
	if !containsASCIIFold(input, "hex") {
		return false
	}

	return contextualHexSurfaceOverlaps(input, changed, hexSpansForPattern(input, hexPayloadPattern)) ||
		contextualHexSurfaceOverlaps(input, changed, shortRuleHexSpans(input))
}

func contextualHexSurfaceOverlaps(input string, changed encodedSpan, spans []encodedSpan) bool {
	for _, span := range spans {
		span.start = contextualHexStart(input, span.start)
		if encodedSpansOverlap(span, changed) {
			return true
		}
	}

	return false
}

func encodedSpansOverlap(left, right encodedSpan) bool {
	return left.start < right.end && right.start < left.end
}

func (d *contextDecoder) admitRuleCandidate(current decodeQueueEntry, candidate string) {
	if d.mayContribute == nil || d.mayContribute(candidate) {
		d.admit(current, candidate)

		return
	}
	if !d.ruleCandidateMayExpand(current.text, candidate) {
		return
	}

	d.deferRuleCandidate(current, candidate)
}

func (d *contextDecoder) admitRuleContextualCandidate(current decodeQueueEntry, span encodedSpan, decoded string) {
	d.admitRuleCandidate(current, replaceDecodedSpan(current.text, span, decoded))
}

func (d *contextDecoder) deferRuleCandidate(current decodeQueueEntry, candidate string) {
	if candidate == current.text {
		return
	}
	if _, exists := d.visited[candidate]; exists {
		return
	}
	data := []byte(candidate)
	if len(data) == 0 || !IsReadableText(data) {
		return
	}
	if len(data) > maxDecodedCandidateLen {
		d.result.Status |= DecodeByteLimit

		return
	}
	if current.depth >= maxDecodeDepth {
		d.result.Status |= DecodeDepthLimit

		return
	}
	if len(d.queue)-d.cursor >= maxDecodeScans {
		d.result.Status |= DecodeScanLimit

		return
	}

	// 확장 전용 중간값은 결과 예산에서 제외하고 기존 scan·depth 한도로 queue를 제한한다.
	d.visited[candidate] = struct{}{}
	d.queue = append(d.queue, decodeQueueEntry{text: candidate, depth: current.depth + 1})
}
