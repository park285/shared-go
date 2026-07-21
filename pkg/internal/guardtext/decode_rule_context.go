package guardtext

const maxShortBase64CandidateLen = minBase64CandidateLen - 1

func decodeCandidatesWithContextForRules(
	input string,
	normalized string,
	mayContribute func(string) bool,
	oversizedWouldBlock func(string, string, []string) bool,
	originalPotential bool,
) DecodeResult {
	needsNormalization := normalized != ""
	if !originalPotential && !needsNormalization {
		return DecodeResult{}
	}
	if originalPotential && !needsNormalization {
		if result, ok := decodeSingleShortRuleContext(input, mayContribute); ok {
			return result
		}
	}

	roots := []string{input}
	if needsNormalization {
		if hasPotentialDecodeSurface(normalized) || hasPlausibleShortRuleDecodeSurface(normalized) {
			roots = append(roots, normalized)
			originalPotential = true
		}
	}
	if !originalPotential {
		return DecodeResult{}
	}

	decoder := contextDecoder{
		result:              DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:               make([]decodeQueueEntry, 0, len(roots)+maxDecodeCandidates),
		visited:             make(map[string]struct{}, len(roots)+maxDecodeCandidates),
		mayContribute:       mayContribute,
		oversizedWouldBlock: oversizedWouldBlock,
	}
	for _, root := range roots {
		if _, exists := decoder.visited[root]; exists {
			continue
		}
		decoder.visited[root] = struct{}{}
		decoder.queue = append(decoder.queue, decodeQueueEntry{text: root})
	}

	for decoder.pending() {
		current := decoder.queue[decoder.cursor]
		decoder.cursor++
		mayContributeOrExpand := func(candidate string) bool {
			return decoder.ruleCandidateMayContributeOrExpand(current.text, candidate)
		}

		standardCandidate := false
		decodeContextSurfaces(
			current.text,
			decodeContextOptions{filterCandidates: true, boundOversizedStandard: true},
			mayContributeOrExpand,
			oversizedWouldBlock,
			&decoder.protectedWork,
			&decoder.scans,
			&decoder.result.Status,
			func(candidate string) {
				standardCandidate = true
				decoder.admitRuleCandidate(current, candidate)
			},
			func(span encodedSpan, decoded string) {
				standardCandidate = true
				decoder.admitRuleContextualCandidate(current, span, decoded)
			},
		)
		if !decoder.result.Complete() {
			continue
		}

		if standardCandidate {
			continue
		}

		decodeShortRuleSurfaces(
			current.text,
			mayContributeOrExpand,
			&decoder.protectedWork,
			&decoder.scans,
			&decoder.result.Status,
			func(span encodedSpan, decoded string) {
				decoder.admitRuleContextualCandidate(current, span, decoded)
			},
		)
	}

	return decoder.result
}

func decodeSingleShortRuleContext(input string, mayContribute func(string) bool) (DecodeResult, bool) {
	if shortRuleFastPathUnsupported(input) {
		return DecodeResult{}, false
	}

	var work protectedDecodeWork
	scans := 0
	status := DecodeStatus(0)
	selectedCandidate := ""
	for position := 0; position < len(input); {
		start := position
		match := nextBase64Candidate(input, position)
		position = match.next
		if len(match.value) < 4 || len(match.value) > maxShortBase64CandidateLen {
			continue
		}
		if !consumeProtectedDecodeWork(&work, &status, len(match.value)) {
			return DecodeResult{Status: status}, true
		}
		if !consumeContextDecodeScan(&scans, &status) {
			return DecodeResult{}, false
		}

		var storage [maxShortBase64CandidateLen]byte
		decoded, err := decodeBase64CandidateInto(storage[:], match.value)
		if err != nil || !IsReadableText(decoded) {
			if looksLikeEmbeddedBase64(match.value) {
				return DecodeResult{}, false
			}
			continue
		}

		decodedText := string(decoded)
		span := encodedSpan{start: start, end: match.next}
		candidate, contributes, nestedStatus := shortRuleCandidateContribution(
			input,
			span,
			decodedText,
			mayContribute,
			&work,
		)
		mergeDecodeStatus(&status, nestedStatus)
		if status != 0 {
			return DecodeResult{Status: status}, true
		}
		if !contributes {
			continue
		}
		if selectedCandidate != "" {
			return DecodeResult{}, false
		}
		selectedCandidate = candidate
	}

	if selectedCandidate == "" {
		return DecodeResult{}, false
	}
	if hasPotentialDecodeSurface(selectedCandidate) || hasPlausibleShortRuleDecodeSurface(selectedCandidate) {
		return DecodeResult{}, false
	}
	return DecodeResult{Candidates: []string{selectedCandidate}}, true
}

func shortRuleFastPathUnsupported(input string) bool {
	return hasPotentialDecodeSurface(input) ||
		containsASCIIFold(input, "hex") && shortHexPayloadPattern.MatchString(input)
}

func shortRuleCandidateContribution(
	input string,
	span encodedSpan,
	decoded string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
) (string, bool, DecodeStatus) {
	contributes, nested, status := matchingDecodedContributionDetails(decoded, mayContribute)
	if status != 0 {
		return "", false, status
	}
	contextBytes := len(input) - (span.end - span.start) + len(decoded)
	if !consumeProtectedContextWork(work, &status, contextBytes) {
		return "", false, status
	}
	contextual := replaceDecodedSpan(input, span, decoded)
	if !contributes {
		contributes, status = matchingContextualDecodedContribution(input, span, decoded, nested, mayContribute)
	}
	if status != 0 || !contributes {
		return contextual, false, status
	}
	if contextBytes > maxDecodedCandidateLen {
		return "", false, DecodeByteLimit
	}
	return contextual, true, 0
}

func hasPlausibleShortRuleDecodeSurface(input string) bool {
	if containsASCIIFold(input, "hex") {
		for _, span := range shortRuleHexSpans(input) {
			if span.end > span.start {
				return true
			}
		}
	}

	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < 4 {
			continue
		}
		if len(match.value) <= maxShortBase64CandidateLen {
			if plausibleShortBase64Value(match.value) {
				return true
			}
			continue
		}
		if looksLikeEmbeddedBase64(match.value) {
			return true
		}
	}

	return false
}

func decodeShortRuleSurfaces(
	input string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	scans *int,
	status *DecodeStatus,
	admitContextual func(encodedSpan, string),
) {
	base64Spans := shortRuleBase64Spans(input, mayContribute, work, status)
	hexSpans := matchingShortHexSpans(input, mayContribute, work, status)
	families := []transformFamily{
		{kind: decodeBase64, input: input, spans: base64Spans},
		{kind: decodeHex, input: input, spans: hexSpans},
	}
	for familiesPending(families) {
		for i := range families {
			family := &families[i]
			if family.next >= len(family.spans) {
				continue
			}
			if !consumeContextDecodeScan(scans, status) {
				return
			}

			span := family.spans[family.next]
			decoded, ok := family.attempt()
			if !ok || !IsReadableText([]byte(decoded)) {
				continue
			}
			if family.kind == decodeHex {
				span.start = contextualHexStart(input, span.start)
			}

			admitContextual(span, decoded)
		}
	}
}
