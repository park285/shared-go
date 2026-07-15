package guardtext

const maxShortBase64CandidateLen = minBase64CandidateLen - 1

func decodeCandidatesWithContextForRules(
	input string,
	mayContribute func(string) bool,
	originalPotential bool,
	needsNormalization bool,
) DecodeResult {
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
		normalized := NormalizeEncodingSyntax(input)
		if normalized != input && (hasPotentialDecodeSurface(normalized) || hasPlausibleShortRuleDecodeSurface(normalized)) {
			roots = append(roots, normalized)
			originalPotential = true
		}
	}
	if !originalPotential {
		return DecodeResult{}
	}

	decoder := contextDecoder{
		result:        DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:         make([]decodeQueueEntry, 0, len(roots)+maxDecodeCandidates),
		visited:       make(map[string]struct{}, len(roots)+maxDecodeCandidates),
		mayContribute: mayContribute,
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

		standardCandidate := false
		decodeContextSurfaces(
			current.text,
			false,
			false,
			nil,
			&decoder.protectedWork,
			&decoder.scans,
			&decoder.result.Status,
			func(candidate string) {
				standardCandidate = true
				decoder.admit(current, candidate)
			},
			func(span encodedSpan, decoded string) {
				standardCandidate = true
				decoder.admitContextual(current, span, decoded)
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
			mayContribute,
			&decoder.protectedWork,
			&decoder.scans,
			&decoder.result.Status,
			func(span encodedSpan, decoded string) { decoder.admitContextual(current, span, decoded) },
		)
	}

	return decoder.result
}

func decodeSingleShortRuleContext(input string, mayContribute func(string) bool) (DecodeResult, bool) {
	if hasPotentialDecodeSurface(input) || containsASCIIFold(input, "hex") && shortHexPayloadPattern.MatchString(input) {
		return DecodeResult{}, false
	}

	var selectedSpan encodedSpan
	selectedDecoded := ""
	for position := 0; position < len(input); {
		start := position
		match := nextBase64Candidate(input, position)
		position = match.next
		if len(match.value) < 4 || len(match.value) > maxShortBase64CandidateLen {
			continue
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
		contributes, status := matchingDecodedContribution(decodedText, mayContribute)
		if status != 0 {
			return DecodeResult{}, false
		}
		if !contributes {
			contextual := replaceDecodedSpan(input, encodedSpan{start: start, end: match.next}, decodedText)
			contributes, status = matchingDecodedContribution(contextual, mayContribute)
			if status != 0 {
				return DecodeResult{}, false
			}
		}
		if !contributes {
			continue
		}
		if selectedDecoded != "" {
			return DecodeResult{}, false
		}
		selectedSpan = encodedSpan{start: start, end: match.next}
		selectedDecoded = decodedText
	}

	if selectedDecoded == "" {
		return DecodeResult{}, false
	}
	candidate := replaceDecodedSpan(input, selectedSpan, selectedDecoded)
	if hasPotentialDecodeSurface(candidate) || hasPlausibleShortRuleDecodeSurface(candidate) {
		return DecodeResult{}, false
	}
	return DecodeResult{Candidates: []string{candidate}}, true
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
