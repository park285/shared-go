package guardtext

func (d *contextDecoder) deferRuleExpansion(current decodeQueueEntry, candidate string) bool {
	if candidate == current.text {
		return false
	}
	if _, exists := d.visited[candidate]; exists {
		return false
	}
	data := []byte(candidate)
	if len(data) == 0 || !IsReadableText(data) {
		return false
	}
	if len(data) > maxDecodedCandidateLen {
		d.result.Status |= DecodeByteLimit

		return false
	}
	if current.depth >= maxDecodeDepth {
		d.result.Status |= DecodeDepthLimit

		return false
	}
	if len(d.queue)-d.cursor >= maxDecodeScans {
		d.result.Status |= DecodeScanLimit

		return false
	}

	// 확장 전용 중간값은 결과 후보·byte 예산을 사용하지 않고 기존 depth·scan 한도로 제한한다.
	d.visited[candidate] = struct{}{}
	d.queue = append(d.queue, decodeQueueEntry{text: candidate, depth: current.depth + 1})

	return true
}

func (d *contextDecoder) observeRuleExpansion(current decodeQueueEntry, candidate decodedContextCandidate) bool {
	if candidate.boundedStandard || candidate.decodedMayContribute || candidate.contextMayContribute {
		return false
	}
	if candidate.kind != decodeBase64 && candidate.kind != decodeHex {
		return false
	}

	contextual := candidate.contextual
	if contextual == "" {
		contextual = replaceDecodedSpan(current.text, candidate.span, candidate.decoded)
	}
	if hasPlausibleShortRuleDecodeSurface(candidate.decoded) {
		return d.deferRuleExpansion(current, contextual)
	}

	replacement := encodedSpan{
		start: candidate.span.start,
		end:   candidate.span.start + len(candidate.decoded),
	}
	// 치환 경계에서 새로 완성되고 실제 rule 기여가 확인된 surface만 확장한다.
	if !d.ruleExpansionCrossBoundaryMayContribute(contextual, replacement) {
		return false
	}

	return d.deferRuleExpansion(current, contextual)
}

func (d *contextDecoder) ruleExpansionCrossBoundaryMayContribute(input string, replacement encodedSpan) bool {
	return d.ruleExpansionCrossBoundaryHexMayContribute(input, replacement) ||
		d.ruleExpansionCrossBoundaryBase64MayContribute(input, replacement)
}

func (d *contextDecoder) ruleExpansionCrossBoundaryHexMayContribute(input string, replacement encodedSpan) bool {
	if !containsASCIIFold(input, "hex") {
		return false
	}
	for _, span := range shortRuleHexSpans(input) {
		surface := span
		surface.start = contextualHexStart(input, span.start)
		if !encodedSpanCrossesReplacementBoundary(surface, replacement) {
			continue
		}
		if !d.consumeRuleExpansionProbe(span.end - span.start) {
			return false
		}
		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}
		if d.ruleExpansionCandidateMayContribute(input, surface, string(decoded)) {
			return true
		}
	}

	return false
}

func (d *contextDecoder) ruleExpansionCrossBoundaryBase64MayContribute(input string, replacement encodedSpan) bool {
	for position := 0; position < len(input); {
		start := position
		match := nextBase64Candidate(input, position)
		position = match.next
		span := encodedSpan{start: start, end: match.next}
		if !plausibleRuleExpansionBase64(match.value) ||
			!encodedSpanCrossesReplacementBoundary(span, replacement) {
			continue
		}
		if !d.consumeRuleExpansionProbe(len(match.value)) {
			return false
		}
		decoded, err := DecodeBase64Candidate(match.value)
		if err != nil || !IsReadableText(decoded) {
			continue
		}
		if d.ruleExpansionCandidateMayContribute(input, span, string(decoded)) {
			return true
		}
	}

	return false
}

func (d *contextDecoder) consumeRuleExpansionProbe(candidateBytes int) bool {
	return consumeProtectedDecodeWork(&d.protectedWork, &d.result.Status, candidateBytes) &&
		consumeContextDecodeScan(&d.scans, &d.result.Status)
}

func (d *contextDecoder) ruleExpansionCandidateMayContribute(input string, span encodedSpan, decoded string) bool {
	_, contributes, status := shortRuleCandidateContribution(input, span, decoded, d.mayContribute, &d.protectedWork)
	mergeDecodeStatus(&d.result.Status, status)

	return contributes && d.result.Complete()
}

func plausibleRuleExpansionBase64(value string) bool {
	if len(value) < 4 {
		return false
	}
	if len(value) <= maxShortBase64CandidateLen {
		return plausibleShortBase64Value(value)
	}

	return looksLikeEmbeddedBase64(value)
}

func encodedSpanCrossesReplacementBoundary(span, replacement encodedSpan) bool {
	return encodedSpansOverlap(span, replacement) &&
		(span.start < replacement.start || span.end > replacement.end)
}

func encodedSpansOverlap(left, right encodedSpan) bool {
	return left.start < right.end && right.start < left.end
}
