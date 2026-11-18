package guardtext

func shortRuleBase64Spans(
	input string,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	var (
		spans []encodedSpan
		seen  map[encodedSpan]struct{}
	)

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		match := nextBase64Candidate(input, i)
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.

		i = match.next

		if len(match.value) < 4 {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		if seenWholes.duplicate(input, whole) {
			continue
		}

		if pathSegments, ok := httpURLPathBase64Segments(input, whole, 4); ok {
			for _, segment := range pathSegments {
				spans, seen = appendShortRuleBase64Whole(spans, seen, input, segment, mayContribute, embeddedContextMayContribute, work, status)
				if !decodeWorkComplete(status) {
					break
				}
			}

			continue
		}

		spans, seen = appendShortRuleBase64Whole(spans, seen, input, whole, mayContribute, embeddedContextMayContribute, work, status)
	}

	return spans
}

func appendShortRuleBase64Whole(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, map[encodedSpan]struct{}) {
	if declaredNonTextDataPayload(input, whole.start) {
		return spans, seen
	}

	value := input[whole.start:whole.end]

	var wholeReadable bool

	if len(value) <= maxShortBase64CandidateLen {
		spans, wholeReadable = appendShortRuleReadableBase64Whole(
			spans,
			input,
			whole,
			mayContribute,
			embeddedContextMayContribute,
			work,
			status,
		)
	} else {
		wholeReadable = readableBase64Span(input, whole, work, status)
	}

	if wholeReadable || !looksLikeEmbeddedBase64(value) || !decodeWorkComplete(status) {
		return spans, seen
	}

	if seen == nil {
		seen = make(map[encodedSpan]struct{}, min(maxDecodeScans+1, 16))
	}

	seen[whole] = struct{}{}

	return appendShortRuleBase64Interiors(spans, seen, input, whole, mayContribute, embeddedContextMayContribute, work, status), seen
}

func appendShortRuleBase64Interiors(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	minimumInterior := 4

	if work != nil && work.regionInteriors {
		minimumInterior = minRegionInteriorBase64Len
	}

	alphabetEnd := base64AlphabetEnd(input, whole)

	for subStart := whole.start; subStart+minimumInterior <= whole.end && len(spans) <= maxDecodeScans && decodeWorkComplete(status); subStart++ {
		maximumEnd := min(whole.end, subStart+maxShortBase64CandidateLen)

		unreadable, ok := base64UnreadablePrefix(input[subStart:max(subStart, min(maximumEnd, alphabetEnd))], work, status)
		if !ok {
			break
		}

		for subEnd := maximumEnd; subEnd-subStart >= minimumInterior && len(spans) <= maxDecodeScans; subEnd-- {
			span := encodedSpan{start: subStart, end: subEnd}
			if span == whole || base64PrefixExcludes(unreadable, min(subEnd, alphabetEnd)-subStart) {
				continue
			}

			spans = appendMatchingShortBase64Span(spans, seen, input, span, mayContribute, embeddedContextMayContribute, work, status)
			if !decodeWorkComplete(status) {
				break
			}
		}
	}

	return spans
}

func appendShortRuleReadableBase64Whole(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, bool) {
	previousCount := len(spans)
	spans, readable := appendEmbeddedProtectedBase64Span(
		spans,
		input,
		whole,
		chargeReadableOnly,
		true,
		true,
		embeddedDirectOrContext,
		mayContribute,
		embeddedContextMayContribute,
		work,
		status,
	)

	if !readable || len(spans) != previousCount || !decodeWorkComplete(status) {
		return spans, readable
	}

	decoded, err := DecodeBase64Candidate(input[whole.start:whole.end])
	if err == nil && nestedShortContextMayContribute(input, whole, string(decoded), mayContribute, work, status) {
		spans = append(spans, whole)
	}

	return spans, readable
}

func nestedShortContextMayContribute(
	input string,
	outer encodedSpan,
	decoded string,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) bool {
	unexploredNesting := false

	for position := 0; position < len(decoded) && decodeWorkComplete(status); {
		start := position //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(decoded, position)

		position = match.next

		if len(match.value) < 4 || len(match.value) > maxShortBase64CandidateLen {
			continue
		}

		inner, err := DecodeBase64Candidate(match.value)
		if err != nil || !IsReadableText(inner) {
			continue
		}

		if !consumeProtectedDecodeWork(work, status, len(match.value)) {
			return false
		}

		nested := replaceDecodedSpan(decoded, encodedSpan{start: start, end: match.next}, string(inner))
		surface := contextualMatchSurface(input, outer, nested)

		if !consumeProtectedContextWork(work, status, len(surface)) {
			return false
		}

		if mayContribute == nil || mayContribute(surface) {
			return true
		}

		if !unexploredNesting {
			unexploredNesting = hasDecodableShortRuleDecodeSurface(nested)
		}
	}

	if unexploredNesting {
		mergeDecodeStatus(status, DecodeDepthLimit)
	}

	return false
}

func readableBase64Span(
	input string,
	span encodedSpan,
	work *protectedDecodeWork,
	status *DecodeStatus,
) bool {
	if (span.end-span.start)%4 == 1 {
		return false
	}

	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return false
	}

	return consumeProtectedDecodeWork(work, status, span.end-span.start)
}

func appendMatchingShortBase64Span(
	spans []encodedSpan,
	seen map[encodedSpan]struct{},
	input string,
	span encodedSpan,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	if _, exists := seen[span]; exists {
		return spans
	}

	seen[span] = struct{}{}

	updated, readable := appendEmbeddedProtectedBase64Span(
		spans,
		input,
		span,
		chargePerAttempt,
		true,
		true,
		embeddedDirectOrContext,
		mayContribute,
		embeddedContextMayContribute,
		work,
		status,
	)
	if !readable || len(updated) != len(spans) || !decodeWorkComplete(status) {
		return updated
	}

	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return updated
	}

	if nestedShortContextMayContribute(input, span, string(decoded), mayContribute, work, status) {
		updated = append(updated, span)
	}

	return updated
}

func hasDecodableShortRuleDecodeSurface(input string) bool {
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

		if len(match.value) > maxShortBase64CandidateLen {
			if looksLikeEmbeddedBase64(match.value) {
				return true
			}

			continue
		}

		var storage [maxShortBase64CandidateLen]byte

		decoded, err := decodeBase64CandidateInto(storage[:], match.value)

		if err == nil && IsReadableText(decoded) {
			return true
		}
	}

	return false
}

func plausibleShortBase64Value(value string) bool {
	if looksLikeEmbeddedBase64(value) {
		return true
	}

	var storage [maxShortBase64CandidateLen]byte

	decoded, err := decodeBase64CandidateInto(storage[:], value)

	return err == nil && IsReadableText(decoded)
}
