package guardtext

const maxShortBase64CandidateLen = minBase64CandidateLen - 1

func decodeCandidatesWithContextForRules(
	input string,
	normalized string,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
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

	decoder := newRuleContextDecoder(roots, mayContribute, embeddedContextMayContribute, oversizedWouldBlock)
	for _, root := range roots {
		if _, exists := decoder.visited[root]; exists {
			continue
		}

		decoder.visited[root] = struct{}{}
		decoder.queue = append(decoder.queue, decodeQueueEntry{text: root})
	}

	for decoder.pending() {
		decoder.expandRuleNext()
	}

	return decoder.result
}

func (d *contextDecoder) expandRuleNext() {
	current := d.queue[d.cursor]
	d.cursor++

	// 정상 변환값이 전역 후보 예산을 선점하지 않도록 rule 기여가 확인된 후보만 admission한다.
	standardCandidate := false

	decodeContextSurfaces(
		current.text,
		decodeContextOptions{filterCandidates: true, boundOversizedStandard: true},
		d.seenWholes,
		d.mayContribute,
		d.embeddedContextMayContribute,
		d.oversizedWouldBlock,
		&d.protectedWork,
		&d.scans,
		&d.result.Status,
		func(candidate decodedContextCandidate) {
			if d.observeRuleExpansion(current, candidate) {
				standardCandidate = true
			}
		},
		func(candidate string) {
			standardCandidate = true

			d.admit(current, candidate)
		},
		func(span encodedSpan, decoded string) {
			standardCandidate = true

			d.admitContextual(current, span, decoded)
		},
	)

	if !d.result.Complete() {
		return
	}

	if standardCandidate {
		return
	}

	decodeShortRuleSurfaces(
		current.text,
		d.seenWholes,
		d.mayContribute,
		d.embeddedContextMayContribute,
		&d.protectedWork,
		&d.scans,
		&d.result.Status,
		func(span encodedSpan, decoded string) { d.admitContextual(current, span, decoded) },
	)
}

func newRuleContextDecoder(
	roots []string,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	oversizedWouldBlock func(string, string, []string) bool,
) contextDecoder {
	decoder := contextDecoder{
		result:                       DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:                        make([]decodeQueueEntry, 0, len(roots)+maxDecodeCandidates),
		visited:                      make(map[string]struct{}, len(roots)+maxDecodeCandidates),
		mayContribute:                mayContribute,
		embeddedContextMayContribute: embeddedContextMayContribute,
		oversizedWouldBlock:          oversizedWouldBlock,
		roots:                        roots,
	}
	for _, root := range roots {
		if decoder.seenWholes = newSpanContextSeen(root); decoder.seenWholes != nil {
			break
		}
	}

	return decoder
}

func decodeSingleShortRuleContext(input string, mayContribute func(string) bool) (DecodeResult, bool) {
	if shortRuleFastPathUnsupported(input) {
		return DecodeResult{}, false
	}

	path := shortRuleFastPath{input: input}

	for position := 0; position < len(input); {
		start := position //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, position)

		position = match.next

		state := path.consume(encodedSpan{start: start, end: match.next}, match.value, mayContribute)
		if state == shortRuleScanFallback {
			return DecodeResult{}, false
		}

		if state == shortRuleScanHalt {
			return DecodeResult{Status: path.status}, true
		}
	}

	return path.selection()
}

type shortRuleFastPath struct {
	input             string
	work              protectedDecodeWork
	scans             int
	status            DecodeStatus
	selectedCandidate string
}

type shortRuleScanState uint8

const (
	shortRuleScanNext shortRuleScanState = iota
	shortRuleScanFallback
	shortRuleScanHalt
)

func (p *shortRuleFastPath) consume(span encodedSpan, value string, mayContribute func(string) bool) shortRuleScanState {
	if len(value) < 4 || len(value) > maxShortBase64CandidateLen {
		return shortRuleScanNext
	}

	var storage [maxShortBase64CandidateLen]byte

	decoded, err := decodeBase64CandidateInto(storage[:], value)

	if err != nil || !IsReadableText(decoded) {
		if looksLikeEmbeddedBase64(value) {
			return shortRuleScanFallback
		}

		return shortRuleScanNext
	}

	if !consumeProtectedDecodeWork(&p.work, &p.status, len(value)) {
		return shortRuleScanHalt
	}

	if !consumeContextDecodeScan(&p.scans, &p.status) {
		return shortRuleScanFallback
	}

	candidate, contributes, nestedStatus := shortRuleCandidateContribution(
		p.input,
		span,
		string(decoded),
		mayContribute,
		&p.work,
	)
	mergeDecodeStatus(&p.status, nestedStatus)

	if p.status != 0 {
		return shortRuleScanHalt
	}

	if !contributes {
		return shortRuleScanNext
	}

	if p.selectedCandidate != "" {
		return shortRuleScanFallback
	}

	p.selectedCandidate = candidate

	return shortRuleScanNext
}

func (p *shortRuleFastPath) selection() (DecodeResult, bool) {
	if p.selectedCandidate == "" {
		return DecodeResult{}, false
	}

	if hasPotentialDecodeSurface(p.selectedCandidate) || hasPlausibleShortRuleDecodeSurface(p.selectedCandidate) {
		return DecodeResult{}, false
	}

	return DecodeResult{Candidates: []string{p.selectedCandidate}}, true
}

func shortRuleFastPathUnsupported(input string) bool {
	return hasPotentialDecodeSurface(input) ||
		containsHexFold(input) && shortHexPayloadPattern.MatchString(input)
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

	contextual, bounded := contextualAdmissionCandidate(input, span, decoded)
	if !bounded {
		return "", false, DecodeByteLimit
	}

	if !contributes {
		contributes, status = matchingContextualDecodedContribution(input, span, decoded, nested, mayContribute, work)
	}

	if status != 0 || !contributes {
		return contextual, false, status
	}

	return contextual, true, 0
}

func hasPlausibleShortRuleDecodeSurface(input string) bool {
	if containsHexFold(input) {
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
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	scans *int,
	status *DecodeStatus,
	admitContextual func(encodedSpan, string),
) {
	base64Spans := shortRuleBase64Spans(input, seenWholes, mayContribute, embeddedContextMayContribute, work, status)
	hexSpans := matchingShortHexSpans(input, mayContribute, embeddedContextMayContribute, work, status)
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
