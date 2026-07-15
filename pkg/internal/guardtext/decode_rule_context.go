package guardtext

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

const maxShortBase64CandidateLen = minBase64CandidateLen - 1

// DecodeCandidatesWithContextForRules expands the standard transform families and
// adds only short Base64/hex fragments that can contribute to a compiled rule.
// All work shares the existing candidate, byte, depth, scan, and protected-work
// budgets, so callers retain deterministic fail-closed behavior.
func DecodeCandidatesWithContextForRules(input string, mayContribute func(string) bool) DecodeResult {
	if mayContribute == nil {
		return DecodeCandidatesWithContext(input)
	}

	standalone := DecodeCandidates(input)
	if !standalone.Complete() {
		return standalone
	}
	if standalone.standaloneBase64 && len(standalone.Candidates) > 0 &&
		!decodeCandidatesContainShortRuleSurface(standalone.Candidates) {
		return standalone
	}

	roots := ruleDecodeRoots(input)
	if len(roots) == 0 {
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
		if !decoder.result.Complete() || !hasPlausibleShortRuleDecodeSurface(current.text) {
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

func ruleDecodeRoots(input string) []string {
	rawPotential := hasRuleDecodeSurface(input)
	normalizeSyntax := needsRuleEncodingSyntaxNormalization(input)
	if !rawPotential && !normalizeSyntax {
		return nil
	}

	var roots []string
	if rawPotential {
		roots = append(roots, input)
	}
	if !normalizeSyntax {
		return roots
	}

	normalized := NormalizeEncodingSyntax(input)
	if normalized != input && hasRuleDecodeSurface(normalized) {
		roots = append(roots, normalized)
	}
	return roots
}

func decodeCandidatesContainShortRuleSurface(candidates []string) bool {
	return slices.ContainsFunc(candidates, hasPlausibleShortRuleDecodeSurface)
}

func hasRuleDecodeSurface(input string) bool {
	return hasPotentialDecodeSurface(input) || hasPlausibleShortRuleDecodeSurface(input)
}

func needsRuleEncodingSyntaxNormalization(input string) bool {
	for _, value := range input {
		if unicode.Is(unicode.Cf, value) {
			return true
		}
		if value < utf8.RuneSelf || unicode.Is(hangulTable, value) || unicode.Is(jamoTable, value) {
			continue
		}
		return true
	}
	return false
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
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if shortBase64CandidateInsideEscape(input, start, match.value) {
			continue
		}
		switch {
		case len(match.value) < 4:
			continue
		case len(match.value) <= maxShortBase64CandidateLen:
			if plausibleShortBase64Value(match.value) {
				return true
			}
		case looksLikeEmbeddedBase64(match.value):
			return true
		}
	}

	return false
}

func shortBase64CandidateInsideEscape(input string, start int, value string) bool {
	if start <= 0 {
		return false
	}
	switch input[start-1] {
	case '%':
		return len(value) >= 2 && isHex(value[0]) && isHex(value[1])
	case '\\':
		return len(value) >= 5 && value[0] == 'u' && allHex(value[1:5])
	default:
		return false
	}
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

func shortRuleBase64Spans(
	input string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/4))
	seen := make(map[encodedSpan]struct{}, min(maxDecodeScans+1, 16))

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < 4 || shortBase64CandidateInsideEscape(input, start, match.value) {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		var wholeReadable bool
		if len(match.value) <= maxShortBase64CandidateLen {
			if !plausibleShortBase64Value(match.value) {
				continue
			}
			spans, wholeReadable = appendProtectedBase64Span(
				spans,
				input,
				whole,
				true,
				true,
				mayContribute,
				work,
				status,
			)
			seen[whole] = struct{}{}
		} else {
			if !looksLikeEmbeddedBase64(match.value) {
				continue
			}
			wholeReadable = readableBase64Span(input, whole, work, status)
		}

		// A readable whole token is the authoritative encoding boundary. Looking
		// inside it would reinterpret valid benign Base64, create dangling-padding
		// variants, and spend the shared candidate budget before useful composition.
		if wholeReadable || !decodeWorkComplete(status) {
			continue
		}
		spans = appendShortRuleBase64Subspans(spans, seen, input, whole, mayContribute, work, status)
	}

	return spans
}

func appendShortRuleBase64Subspans(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	for subStart := whole.start; subStart < whole.end && len(spans) <= maxDecodeScans && decodeWorkComplete(status); subStart++ {
		maximumEnd := min(whole.end, subStart+maxShortBase64CandidateLen)
		for subEnd := maximumEnd; subEnd-subStart >= 4 && len(spans) <= maxDecodeScans; subEnd-- {
			span := encodedSpan{start: subStart, end: subEnd}
			if span == whole {
				continue
			}
			spans = appendMatchingShortBase64Span(spans, seen, input, span, mayContribute, work, status)
			if !decodeWorkComplete(status) {
				return spans
			}
		}
	}
	return spans
}

func readableBase64Span(
	input string,
	span encodedSpan,
	work *protectedDecodeWork,
	status *DecodeStatus,
) bool {
	if (span.end-span.start)%4 == 1 || !consumeProtectedDecodeWork(work, status, span.end-span.start) {
		return false
	}
	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	return err == nil && IsReadableText(decoded)
}

func appendMatchingShortBase64Span(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	span encodedSpan,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	if _, exists := seen[span]; exists {
		return spans
	}
	seen[span] = struct{}{}

	updated, _ := appendProtectedBase64Span(
		spans,
		input,
		span,
		true,
		true,
		mayContribute,
		work,
		status,
	)
	return updated
}

func matchingShortHexSpans(
	input string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	matches := shortHexPayloadPattern.FindAllStringSubmatchIndex(input, maxProtectedDecodeTries+1)
	spans := make([]encodedSpan, 0, min(len(matches), maxDecodeScans+1))
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		span := encodedSpan{start: match[2], end: match[3]}
		if decodedHexByteCount(input[span.start:span.end]) >= 4 {
			continue
		}
		if !consumeProtectedDecodeWork(work, status, span.end-span.start) {
			break
		}
		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}
		contributes, nestedStatus := matchingDecodedContribution(string(decoded), mayContribute)
		mergeDecodeStatus(status, nestedStatus)
		if !contributes && decodeWorkComplete(status) {
			contextSpan := span
			contextSpan.start = contextualHexStart(input, span.start)
			contextBytes := len(input) - (contextSpan.end - contextSpan.start) + len(decoded)
			if contextBytes > maxDecodedCandidateLen {
				*status |= DecodeByteLimit
				break
			}
			if !consumeProtectedContextWork(work, status, contextBytes) {
				break
			}
			contextual := replaceDecodedSpan(input, contextSpan, string(decoded))
			contributes, nestedStatus = matchingDecodedContribution(contextual, mayContribute)
			mergeDecodeStatus(status, nestedStatus)
		}
		if contributes {
			spans = append(spans, span)
		}
		if len(spans) > maxDecodeScans || !decodeWorkComplete(status) {
			break
		}
	}
	if len(matches) > maxProtectedDecodeTries {
		markProtectedDecodeWorkIncomplete(status)
	}

	return spans
}

func shortRuleHexSpans(input string) []encodedSpan {
	matches := shortHexPayloadPattern.FindAllStringSubmatchIndex(input, maxDecodeScans+1)
	spans := make([]encodedSpan, 0, len(matches))
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		span := encodedSpan{start: match[2], end: match[3]}
		if decodedHexByteCount(input[span.start:span.end]) < 4 {
			spans = append(spans, span)
		}
	}
	return spans
}

func decodedHexByteCount(payload string) int {
	digits := 0
	for i := range len(payload) {
		if isHex(payload[i]) {
			digits++
		}
	}
	return digits / 2
}

func plausibleShortBase64Value(value string) bool {
	if len(value) < 4 || len(value) > maxShortBase64CandidateLen || len(value)%4 == 1 {
		return false
	}
	if looksLikeEmbeddedBase64(value) {
		return true
	}

	hasLower := false
	uppercaseAfterFirst := 0
	for i := range len(value) {
		switch {
		case value[i] >= 'a' && value[i] <= 'z':
			hasLower = true
		case value[i] >= 'A' && value[i] <= 'Z' && i > 0:
			uppercaseAfterFirst++
		}
	}
	return hasLower && uppercaseAfterFirst > 0
}
