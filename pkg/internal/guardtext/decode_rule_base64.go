package guardtext

func shortRuleBase64Spans(
	input string,
	mayContribute func(string) bool,
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
		if isOpaqueBase64Envelope(input, whole) {
			continue
		}
		var wholeReadable bool
		if len(match.value) <= maxShortBase64CandidateLen {
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
		} else {
			wholeReadable = readableBase64Span(input, whole, work, status)
		}

		// A readable whole token is the authoritative encoding boundary. Looking
		// inside it would reinterpret valid benign Base64, create dangling-padding
		// variants, and spend the shared candidate budget before useful composition.
		if wholeReadable || !looksLikeEmbeddedBase64(match.value) || !decodeWorkComplete(status) {
			continue
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
				spans = appendMatchingShortBase64Span(spans, seen, input, span, mayContribute, work, status)
				if !decodeWorkComplete(status) {
					break
				}
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

func plausibleShortBase64Value(value string) bool {
	if looksLikeEmbeddedBase64(value) {
		return true
	}
	var storage [maxShortBase64CandidateLen]byte
	decoded, err := decodeBase64CandidateInto(storage[:], value)
	return err == nil && IsReadableText(decoded)
}
