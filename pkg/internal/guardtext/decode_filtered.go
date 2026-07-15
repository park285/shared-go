package guardtext

import (
	"slices"
	"strings"
)

func protectedBase64Spans(input string, mayContribute func(string) bool, work *protectedDecodeWork, status *DecodeStatus) []encodedSpan {
	return filteredBase64Spans(input, 4, false, false, false, mayContribute, work, status)
}

func matchingBase64Spans(input string, mayContribute func(string) bool, work *protectedDecodeWork, status *DecodeStatus) []encodedSpan {
	return filteredBase64Spans(input, minBase64CandidateLen, true, true, true, mayContribute, work, status)
}

func filteredBase64Spans(
	input string,
	minimum int,
	strictContribution bool,
	matchContext bool,
	recordWholeTransforms bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/4))
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < minimum {
			continue
		}
		if recordWholeTransforms {
			recordSupportedWholeBase64Transform(match.value, work, status)
			if !decodeWorkComplete(status) {
				break
			}
		}
		enumerateBoundaries := looksLikeEmbeddedBase64(match.value)
		spans = appendProtectedBase64Boundaries(
			spans,
			input,
			encodedSpan{start: start, end: match.next},
			minimum,
			enumerateBoundaries,
			strictContribution,
			matchContext,
			mayContribute,
			work,
			status,
		)
	}
	return spans
}

func recordSupportedWholeBase64Transform(value string, work *protectedDecodeWork, status *DecodeStatus) {
	decoded, err := DecodeBase64Candidate(value)
	if err != nil || !IsReadableText(decoded) {
		return
	}
	if len(decoded) > maxDecodedCandidateLen || work.supportedBytes+len(decoded) > maxDecodedTotalBytes {
		*status |= DecodeByteLimit

		return
	}
	if work.supportedCandidates >= maxDecodeCandidates {
		*status |= DecodeCandidateLimit

		return
	}
	work.supportedCandidates++
	work.supportedBytes += len(decoded)
}

func appendProtectedBase64Boundaries(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	enumerateBoundaries bool,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	spans, wholeReadable := appendProtectedBase64Span(spans, input, whole, strictContribution, matchContext, mayContribute, work, status)
	wholeIsPadded := whole.end > whole.start && input[whole.end-1] == '='
	if len(spans) > maxDecodeScans || !decodeWorkComplete(status) || !enumerateBoundaries || wholeReadable && wholeIsPadded {
		return spans
	}
	spans = appendProtectedBase64Prefixes(spans, input, whole, minimum, strictContribution, matchContext, mayContribute, work, status)
	if protectedBase64EnumerationDone(spans, status) {
		return spans
	}
	spans = appendProtectedBase64Suffixes(spans, input, whole, minimum, strictContribution, matchContext, mayContribute, work, status)
	if protectedBase64EnumerationDone(spans, status) {
		return spans
	}

	return appendProtectedBase64Interiors(spans, input, whole, minimum, strictContribution, matchContext, mayContribute, work, status)
}

func appendProtectedBase64Prefixes(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	for end := whole.end - 1; end-whole.start >= minimum && len(spans) <= maxDecodeScans; end-- {
		spans, _ = appendProtectedBase64Span(spans, input, encodedSpan{start: whole.start, end: end}, strictContribution, matchContext, mayContribute, work, status)
		if !decodeWorkComplete(status) {
			return spans
		}
	}

	return spans
}

func appendProtectedBase64Suffixes(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	for start := whole.start + 1; whole.end-start >= minimum && len(spans) <= maxDecodeScans; start++ {
		spans, _ = appendProtectedBase64Span(spans, input, encodedSpan{start: start, end: whole.end}, strictContribution, matchContext, mayContribute, work, status)
		if !decodeWorkComplete(status) {
			return spans
		}
	}

	return spans
}

func appendProtectedBase64Interiors(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	for start := whole.start + 1; whole.end-start >= minimum+1 && len(spans) <= maxDecodeScans; start++ {
		for end := whole.end - 1; end-start >= minimum && len(spans) <= maxDecodeScans; end-- {
			spans, _ = appendProtectedBase64Span(spans, input, encodedSpan{start: start, end: end}, strictContribution, matchContext, mayContribute, work, status)
			if !decodeWorkComplete(status) {
				return spans
			}
		}
	}

	return spans
}

func protectedBase64EnumerationDone(spans []encodedSpan, status *DecodeStatus) bool {
	return len(spans) > maxDecodeScans || !decodeWorkComplete(status)
}

func appendProtectedBase64Span(
	spans []encodedSpan,
	input string,
	span encodedSpan,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, bool) {
	if (span.end-span.start)%4 == 1 || !consumeProtectedDecodeWork(work, status, span.end-span.start) {
		return spans, false
	}
	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return spans, false
	}
	contribution := protectedDecodedContribution
	if strictContribution {
		contribution = matchingDecodedContribution
	}
	contributes, nestedStatus := contribution(string(decoded), mayContribute)
	mergeDecodeStatus(status, nestedStatus)
	if !contributes && matchContext {
		contextBytes := len(input) - (span.end - span.start) + len(decoded)
		if contextBytes > maxDecodedCandidateLen {
			*status |= DecodeByteLimit

			return spans, true
		}
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return spans, true
		}
		contextual := replaceDecodedSpan(input, span, string(decoded))
		contributes, nestedStatus = contribution(contextual, mayContribute)
		mergeDecodeStatus(status, nestedStatus)
	}
	if !contributes {
		return spans, true
	}
	if len(spans) == 0 || spans[len(spans)-1] != span {
		spans = append(spans, span)
	}

	return spans, true
}

func protectedHexSpans(input string, mayContribute func(string) bool, work *protectedDecodeWork, status *DecodeStatus) []encodedSpan {
	matches := shortHexPayloadPattern.FindAllStringSubmatchIndex(input, maxProtectedDecodeTries+1)
	spans := make([]encodedSpan, 0, min(len(matches), maxDecodeScans+1))
	for _, match := range matches {
		if len(match) != 4 || !consumeProtectedDecodeWork(work, status, match[3]-match[2]) {
			break
		}
		span := encodedSpan{start: match[2], end: match[3]}
		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}
		contributes, nestedStatus := protectedDecodedContribution(string(decoded), mayContribute)
		mergeDecodeStatus(status, nestedStatus)
		if !contributes {
			continue
		}
		spans = append(spans, span)
		if len(spans) > maxDecodeScans {
			break
		}
	}
	if len(matches) > maxProtectedDecodeTries {
		markProtectedDecodeWorkIncomplete(status)
	}

	return spans
}

func protectedDecodedContribution(decoded string, mayContribute func(string) bool) (bool, DecodeStatus) {
	return decodedContribution(decoded, mayContribute, true)
}

func matchingDecodedContribution(decoded string, mayContribute func(string) bool) (bool, DecodeStatus) {
	return decodedContribution(decoded, mayContribute, false)
}

func decodedContribution(decoded string, mayContribute func(string) bool, retainPotentialNested bool) (bool, DecodeStatus) {
	if mayContribute == nil || mayContribute(decoded) {
		return true, 0
	}
	nested := DecodeCandidates(decoded)
	if !nested.Complete() {
		return false, nested.Status
	}
	if slices.ContainsFunc(nested.Candidates, mayContribute) {
		return true, 0
	}
	if !retainPotentialNested && len(nested.Candidates) > 0 {
		return true, 0
	}
	if shortNestedDecodeMayContribute(decoded, mayContribute) {
		return true, 0
	}
	if retainPotentialNested && hasPlausibleShortDecodeSurface(decoded) {
		return true, 0
	}

	return false, 0
}

func shortNestedDecodeMayContribute(input string, mayContribute func(string) bool) bool {
	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < 4 || !looksLikeEmbeddedBase64(match.value) {
			continue
		}
		decoded, err := DecodeBase64Candidate(match.value)
		if err == nil && IsReadableText(decoded) && mayContribute(string(decoded)) {
			return true
		}
	}
	for _, span := range hexSpansForPattern(input, shortHexPayloadPattern) {
		decoded, err := decodeHexPayload(input[span.start:span.end])
		if err == nil && IsReadableText(decoded) && mayContribute(string(decoded)) {
			return true
		}
	}

	return false
}

func hasPlausibleShortDecodeSurface(input string) bool {
	if shortHexPayloadPattern.MatchString(input) {
		return true
	}
	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < 4 {
			continue
		}
		if looksLikeEmbeddedBase64(match.value) {
			return true
		}
		decoded, err := DecodeBase64Candidate(match.value)
		if err == nil && IsReadableText(decoded) {
			return true
		}
	}

	return false
}

func consumeProtectedDecodeWork(work *protectedDecodeWork, status *DecodeStatus, bytes int) bool {
	if work == nil {
		return true
	}
	if work.tries >= maxProtectedDecodeTries || work.bytes+bytes > maxProtectedDecodeBytes {
		markProtectedDecodeWorkIncomplete(status)

		return false
	}
	work.tries++
	work.bytes += bytes

	return true
}

func consumeProtectedContextWork(work *protectedDecodeWork, status *DecodeStatus, bytes int) bool {
	if work == nil {
		return true
	}
	if bytes < 0 || work.contextBytes+bytes > maxProtectedContextBytes {
		if status != nil {
			*status |= DecodeByteLimit
		}

		return false
	}
	work.contextBytes += bytes

	return true
}

func markProtectedDecodeWorkIncomplete(status *DecodeStatus) {
	if status != nil {
		*status |= DecodeScanLimit
	}
}

func mergeDecodeStatus(status *DecodeStatus, nested DecodeStatus) {
	if status != nil {
		*status |= nested
	}
}

func decodeWorkComplete(status *DecodeStatus) bool {
	return status == nil || *status == 0
}

func looksLikeEmbeddedBase64(value string) bool {
	if strings.HasSuffix(value, "=") {
		return true
	}
	if len(value) >= 32 && (allHex(value) || looksLikeDecoratedHexDigest(value)) {
		return false
	}
	hasLower := false
	hasDigit := false
	uppercaseAfterFirst := 0
	for i := range len(value) {
		switch {
		case value[i] >= 'a' && value[i] <= 'z':
			hasLower = true
		case value[i] >= 'A' && value[i] <= 'Z':
			if i > 0 {
				uppercaseAfterFirst++
			}
		case value[i] >= '0' && value[i] <= '9':
			hasDigit = true
		}
	}
	return hasLower && (hasDigit || uppercaseAfterFirst >= 3)
}

func looksLikeDecoratedHexDigest(value string) bool {
	left, right, ok := strings.Cut(value, "-")
	if !ok || strings.ContainsRune(right, '-') {
		return false
	}
	if isStandardHexDigest(left) && strings.EqualFold(right, "artifact") {
		return true
	}

	var wantBytes int
	switch strings.ToLower(left) {
	case "md5":
		wantBytes = 16
	case "sha1":
		wantBytes = 20
	case "sha224":
		wantBytes = 28
	case "sha256":
		wantBytes = 32
	case "sha384":
		wantBytes = 48
	case "sha512":
		wantBytes = 64
	default:
		return false
	}

	return len(right) == wantBytes*2 && allHex(right)
}

func isStandardHexDigest(value string) bool {
	switch len(value) {
	case 32, 40, 56, 64, 96, 128:
		return allHex(value)
	default:
		return false
	}
}
