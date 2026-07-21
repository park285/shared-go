package guardtext

func (d *contextDecoder) ruleCandidateMayContributeOrExpand(source, candidate string) bool {
	if d.mayContribute == nil || d.mayContribute(candidate) {
		return true
	}

	return hasRuleDecodeSurface(transformedCandidateRegion(source, candidate))
}

func hasRuleDecodeSurface(input string) bool {
	return hasPotentialDecodeSurface(input) || hasPlausibleShortRuleDecodeSurface(input)
}

func transformedCandidateRegion(source, candidate string) string {
	if source == candidate {
		return ""
	}

	for position := 0; position < len(source); {
		start := position
		match := nextBase64Candidate(source, position)
		position = match.next
		if len(match.value) < 4 {
			continue
		}
		if replacement, ok := replacementRegion(source, candidate, encodedSpan{start: start, end: match.next}); ok {
			return replacement
		}
	}
	for _, span := range hexSpansForPattern(source, hexPayloadPattern) {
		span.start = contextualHexStart(source, span.start)
		if replacement, ok := replacementRegion(source, candidate, span); ok {
			return replacement
		}
	}
	for _, span := range shortRuleHexSpans(source) {
		span.start = contextualHexStart(source, span.start)
		if replacement, ok := replacementRegion(source, candidate, span); ok {
			return replacement
		}
	}

	return candidate
}

func replacementRegion(source, candidate string, span encodedSpan) (string, bool) {
	if span.start < 0 || span.start > span.end || span.end > len(source) {
		return "", false
	}
	suffixBytes := len(source) - span.end
	candidateEnd := len(candidate) - suffixBytes
	if candidateEnd < span.start || len(candidate) < suffixBytes ||
		candidate[:span.start] != source[:span.start] || candidate[candidateEnd:] != source[span.end:] {
		return "", false
	}

	return candidate[span.start:candidateEnd], true
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
