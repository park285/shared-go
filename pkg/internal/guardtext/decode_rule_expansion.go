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
	if !hasPlausibleShortRuleDecodeSurface(candidate.decoded) {
		return false
	}

	contextual := candidate.contextual
	if contextual == "" {
		contextual = replaceDecodedSpan(current.text, candidate.span, candidate.decoded)
	}

	return d.deferRuleExpansion(current, contextual)
}
