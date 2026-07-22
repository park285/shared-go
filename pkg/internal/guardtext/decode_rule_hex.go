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
		if !consumeProtectedDecodeWork(work, status, span.end-span.start) {
			break
		}
		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}
		contributes, nested, nestedStatus := matchingDecodedContributionDetails(string(decoded), mayContribute)
		mergeDecodeStatus(status, nestedStatus)
		if !contributes && decodeWorkComplete(status) {
			contextSpan := span
			contextSpan.start = contextualHexStart(input, span.start)
			contextBytes := len(input) - (contextSpan.end - contextSpan.start) + len(decoded)
			if embeddedContextMayContribute != nil {
				contributes = embeddedContextContributes(input, contextSpan, string(decoded), nested, embeddedContextMayContribute)
			} else {
				if !consumeProtectedContextWork(work, status, contextBytes) {
					break
				}
				contributes, nestedStatus = matchingContextualDecodedContribution(input, contextSpan, string(decoded), nested, mayContribute)
				mergeDecodeStatus(status, nestedStatus)
			}
			if contributes && contextBytes > maxDecodedCandidateLen {
				*status |= DecodeByteLimit
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
