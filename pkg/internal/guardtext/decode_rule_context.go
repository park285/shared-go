package guardtext

const maxShortBase64CandidateLen = minBase64CandidateLen - 1

// DecodeCandidatesWithContextForRules expands the standard transform families and
// adds only short Base64/hex fragments that can contribute to a compiled rule.
// All work shares the existing candidate, byte, depth, scan, and protected-work
// budgets, so callers retain deterministic fail-closed behavior.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}

	roots := []string{input}
	normalized := NormalizeEncodingSyntax(input)
	if normalized != input {
		roots = append(roots, normalized)
	}
	potential := false
	for _, root := range roots {
		if hasPotentialDecodeSurface(root) || hasPlausibleShortRuleDecodeSurface(root) {
			potential = true
			break
		}
	}
	if !potential {
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

		decodeContextSurfaces(
			current.text,
			false,
			false,
			nil,
			&decoder.protectedWork,
			&decoder.scans,
			&decoder.result.Status,
			func(candidate string) { decoder.admit(current, candidate) },
			func(span encodedSpan, decoded string) { decoder.admitContextual(current, span, decoded) },
		)
		if !decoder.result.Complete() {
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

func hasPlausibleShortRuleDecodeSurface(input string) bool {
	for _, span := range shortRuleHexSpans(input) {
		if span.end > span.start {
			return true
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
