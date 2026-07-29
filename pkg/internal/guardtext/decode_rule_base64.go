package guardtext

func shortRuleBase64Spans(
	input string,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	var spans []encodedSpan
	var seen map[encodedSpan]struct{}

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		match := nextBase64Candidate(input, i)
		start := i
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
		spans, wholeReadable = appendEmbeddedProtectedBase64Span(
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
	} else {
		wholeReadable = readableBase64Span(input, whole, work, status)
	}

	// 읽을 수 있는 전체 토큰은 권위 있는 인코딩 경계이므로 내부를 재해석하지 않는다.
	if wholeReadable || !looksLikeEmbeddedBase64(value) || !decodeWorkComplete(status) {
		return spans, seen
	}
	if seen == nil {
		seen = make(map[encodedSpan]struct{}, min(maxDecodeScans+1, 16))
	}
	seen[whole] = struct{}{}

	for subStart := whole.start; subStart < whole.end && len(spans) <= maxDecodeScans && decodeWorkComplete(status); subStart++ {
		maximumEnd := min(whole.end, subStart+maxShortBase64CandidateLen)
		for subEnd := maximumEnd; subEnd-subStart >= 4 && len(spans) <= maxDecodeScans; subEnd-- {
			span := encodedSpan{start: subStart, end: subEnd}
			if span == whole {
				continue
			}
			spans = appendMatchingShortBase64Span(spans, seen, input, span, mayContribute, embeddedContextMayContribute, work, status)
			if !decodeWorkComplete(status) {
				break
			}
		}
	}

	return spans, seen
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

	updated, _ := appendEmbeddedProtectedBase64Span(
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
	return updated
}

func plausibleShortBase64Value(value string) bool {
	if looksLikeEmbeddedBase64(value) {
		return true
	}
	var storage [maxShortBase64CandidateLen]byte
	decoded, err := decodeBase64CandidateInto(storage[:], value)
	return err == nil && IsReadableText(decoded)
}
