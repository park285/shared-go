package guardtext

func matchingShortHexSpans(
	input string,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
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

		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}

		if !consumeProtectedDecodeWork(work, status, span.end-span.start) {
			break
		}

		decodedText := string(decoded)

		contributes, nested, nestedStatus := matchingDecodedContributionDetails(decodedText, mayContribute)
		mergeDecodeStatus(status, nestedStatus)

		if !contributes && decodeWorkComplete(status) {
			var halted bool

			contributes, halted = shortHexContextContributes(
				input,
				span,
				decodedText,
				nested,
				mayContribute,
				embeddedContextMayContribute,
				work,
				status,
			)
			if halted {
				break
			}
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

func shortHexContextContributes(
	input string,
	span encodedSpan,
	decoded string,
	nested DecodeResult,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) (bool, bool) {
	contextSpan := span

	contextSpan.start = contextualHexStart(input, span.start)

	contextBytes := len(input) - (contextSpan.end - contextSpan.start) + len(decoded)

	var contributes bool

	if embeddedContextMayContribute != nil {
		contributes = embeddedContextContributes(input, contextSpan, decoded, nested, embeddedContextMayContribute)
	} else {
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return false, true
		}

		var nestedStatus DecodeStatus

		contributes, nestedStatus = matchingContextualDecodedContribution(input, contextSpan, decoded, nested, mayContribute, work)
		mergeDecodeStatus(status, nestedStatus)
	}

	if contributes && contextBytes > maxDecodedCandidateLen {
		*status |= DecodeByteLimit

		return false, true
	}

	return contributes, false
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
