package guardtext

func (d *contextDecoder) ruleCandidateMayContributeOrExpand(source, candidate string) bool {
	if d.mayContribute == nil || d.mayContribute(candidate) {
		return true
	}

	changed, ok := transformedCandidateRange(source, candidate)
	if !ok {
		changed = encodedSpan{end: len(candidate)}
	}

	return ruleDecodeSurfaceOverlaps(candidate, changed)
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
	for _, span := range percentSpans(input) {
		if encodedSpansOverlap(span, changed) {
			return true
		}
	}
	for _, span := range htmlEntitySpans(input) {
		if encodedSpansOverlap(span, changed) {
			return true
		}
	}
	for _, span := range jsonEscapeSpans(input) {
		if encodedSpansOverlap(span, changed) {
			return true
		}
	}

	return false
}

func hexRuleSurfaceOverlaps(input string, changed encodedSpan) bool {
	for _, pattern := range []struct {
		spans []encodedSpan
	}{
		{spans: hexSpansForPattern(input, hexPayloadPattern)},
		{spans: shortRuleHexSpans(input)},
	} {
		for _, span := range pattern.spans {
			span.start = contextualHexStart(input, span.start)
			if encodedSpansOverlap(span, changed) {
				return true
			}
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
