package guardtext

import (
	"slices"
	"strings"
)

type embeddedContributionMode uint8

const (
	embeddedContextOnly embeddedContributionMode = iota
	embeddedDirectOrContext
)

func protectedBase64Spans(
	input string,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	return filteredBase64Spans(input, 4, filteredBase64Options{enumerateReadable: true}, seenWholes, mayContribute, embeddedContextMayContribute, work, status)
}

type filteredBase64Options struct {
	strictContribution                bool
	matchContext                      bool
	enumerateReadable                 bool
	emitNonContributingReadableWholes bool
}

func ruleBase64Spans(
	input string,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	return filteredBase64Spans(
		input,
		minBase64CandidateLen,
		filteredBase64Options{
			strictContribution:                true,
			matchContext:                      true,
			emitNonContributingReadableWholes: true,
		},
		seenWholes,
		mayContribute,
		embeddedContextMayContribute,
		work,
		status,
	)
}

func filteredBase64Spans(
	input string,
	minimum int,
	options filteredBase64Options,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/4))
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans && decodeWorkComplete(status); {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minimum {
			continue
		}

		if declaredNonTextDataPayload(input, start) {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		if seenWholes.duplicate(input, whole) {
			continue
		}

		enumerateBoundaries := looksLikeEmbeddedBase64(match.value)

		spans = appendProtectedBase64Boundaries(
			spans,
			input,
			whole,
			minimum,
			enumerateBoundaries,
			options,
			mayContribute,
			embeddedContextMayContribute,
			work,
			status,
		)
	}

	return spans
}

func appendProtectedBase64Boundaries(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	enumerateBoundaries bool,
	options filteredBase64Options,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	spans, wholeReadable := appendProtectedBase64Span(spans, input, whole, chargeReadableOnly, options.strictContribution, options.matchContext, options.emitNonContributingReadableWholes, mayContribute, work, status)
	wholeIsPadded := whole.end > whole.start && input[whole.end-1] == '='
	skipReadableWhole := wholeReadable && (wholeIsPadded || !options.enumerateReadable)

	if len(spans) > maxDecodeScans || !decodeWorkComplete(status) || !enumerateBoundaries || skipReadableWhole {
		return spans
	}

	if pathSegments, ok := httpURLPathBase64Segments(input, whole, minimum); ok {
		for _, segment := range pathSegments {
			spans = appendProtectedBase64Boundaries(
				spans,
				input,
				segment,
				minimum,
				looksLikeEmbeddedBase64(input[segment.start:segment.end]),
				options,
				mayContribute,
				embeddedContextMayContribute,
				work,
				status,
			)
			if protectedBase64EnumerationDone(spans, status) {
				return spans
			}
		}

		return spans
	}

	spans = appendProtectedBase64Prefixes(spans, input, whole, minimum, options.strictContribution, options.matchContext, mayContribute, embeddedContextMayContribute, work, status)
	if protectedBase64EnumerationDone(spans, status) {
		return spans
	}

	spans = appendProtectedBase64Suffixes(spans, input, whole, minimum, options.strictContribution, options.matchContext, mayContribute, embeddedContextMayContribute, work, status)
	if protectedBase64EnumerationDone(spans, status) {
		return spans
	}

	return appendProtectedBase64Interiors(spans, input, whole, minimum, options.strictContribution, options.matchContext, mayContribute, embeddedContextMayContribute, work, status)
}

func httpURLPathBase64Segments(input string, whole encodedSpan, minimum int) ([]encodedSpan, bool) {
	if whole.start < 0 || whole.start >= whole.end || whole.end > len(input) || !strings.ContainsRune(input[whole.start:whole.end], '/') {
		return nil, false
	}

	lookbackStart := max(0, whole.start-maxDecodedCandidateLen)
	prefix := input[lookbackStart:whole.start]
	schemeStart := lastHTTPURLScheme(prefix)

	if schemeStart < 0 {
		return nil, false
	}

	schemeStart += lookbackStart

	between := input[schemeStart:whole.start]

	if strings.ContainsAny(between, "\t\n\r \"'<>[]{}") || strings.ContainsAny(between, "?#") {
		return nil, false
	}

	segments := make([]encodedSpan, 0, 3)
	segmentStart := whole.start

	for position := whole.start; position < whole.end; position++ {
		if input[position] != '/' {
			continue
		}

		if position-segmentStart >= minimum {
			segments = append(segments, encodedSpan{start: segmentStart, end: position})
		}

		segmentStart = position + 1
	}

	if whole.end-segmentStart >= minimum {
		segments = append(segments, encodedSpan{start: segmentStart, end: whole.end})
	}

	return segments, true
}

func lastHTTPURLScheme(input string) int {
	for start := len(input) - len("http://"); start >= 0; start-- {
		remaining := input[start:]
		if len(remaining) >= len("https://") && strings.EqualFold(remaining[:len("https://")], "https://") {
			return start
		}

		if strings.EqualFold(remaining[:len("http://")], "http://") {
			return start
		}
	}

	return -1
}

func appendProtectedBase64Prefixes(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	strictContribution bool,
	matchContext bool,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	alphabetEnd := base64AlphabetEnd(input, whole)

	unreadable, ok := base64UnreadablePrefix(input[whole.start:alphabetEnd], work, status)
	if !ok {
		return spans
	}

	for end := min(whole.end-1, base64UnexcludedEnd(unreadable, whole.start, alphabetEnd)); end-whole.start >= minimum && len(spans) <= maxDecodeScans; end-- {
		spans, _ = appendEmbeddedProtectedBase64Span(spans, input, encodedSpan{start: whole.start, end: end}, chargePerAttempt, strictContribution, matchContext, embeddedContextOnly, mayContribute, embeddedContextMayContribute, work, status)
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
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	alphabetEnd := base64AlphabetEnd(input, whole)

	for start := whole.start + 1; whole.end-start >= minimum && len(spans) <= maxDecodeScans; start++ {
		unreadable, ok := base64UnreadablePrefix(input[start:max(start, alphabetEnd)], work, status)
		if !ok {
			return spans
		}

		if base64PrefixExcludes(unreadable, alphabetEnd-start) {
			continue
		}

		spans, _ = appendEmbeddedProtectedBase64Span(spans, input, encodedSpan{start: start, end: whole.end}, chargePerAttempt, strictContribution, matchContext, embeddedContextOnly, mayContribute, embeddedContextMayContribute, work, status)
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
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []encodedSpan {
	alphabetEnd := base64AlphabetEnd(input, whole)

	for start := whole.start + 1; whole.end-start >= minimum+1 && len(spans) <= maxDecodeScans; start++ {
		unreadable, ok := base64UnreadablePrefix(input[start:max(start, min(whole.end-1, alphabetEnd))], work, status)
		if !ok {
			return spans
		}

		for end := min(whole.end-1, base64UnexcludedEnd(unreadable, start, alphabetEnd)); end-start >= minimum && len(spans) <= maxDecodeScans; end-- {
			spans, _ = appendEmbeddedProtectedBase64Span(spans, input, encodedSpan{start: start, end: end}, chargePerAttempt, strictContribution, matchContext, embeddedContextOnly, mayContribute, embeddedContextMayContribute, work, status)
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
	charge decodeChargePolicy,
	strictContribution bool,
	matchContext bool,
	emitNonContributingReadable bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, bool) {
	decoded, readable := decodeProtectedBase64Span(input, span, charge, work, status)
	if !readable {
		return spans, false
	}

	var (
		contributes  bool
		nested       DecodeResult
		nestedStatus DecodeStatus
	)

	if strictContribution {
		contributes, nested, nestedStatus = matchingDecodedContributionDetails(decoded, mayContribute)
	} else {
		contributes, nestedStatus = protectedDecodedContribution(decoded, mayContribute)
	}

	mergeDecodeStatus(status, nestedStatus)

	if !contributes && matchContext {
		contextBytes := len(input) - (span.end - span.start) + len(decoded)
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return spans, true
		}

		if strictContribution {
			contributes, nestedStatus = matchingContextualDecodedContribution(input, span, decoded, nested, mayContribute, work)
		} else {
			contributes, nestedStatus = contextualWindowContribution(contextualMatchSurface(input, span, decoded), mayContribute)
		}

		mergeDecodeStatus(status, nestedStatus)
	}

	if !contributes && !emitNonContributingReadable {
		return spans, true
	}

	if len(spans) == 0 || spans[len(spans)-1] != span {
		spans = append(spans, span)
	}

	return spans, true
}

func appendEmbeddedProtectedBase64Span(
	spans []encodedSpan,
	input string,
	span encodedSpan,
	charge decodeChargePolicy,
	strictContribution bool,
	matchContext bool,
	mode embeddedContributionMode,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) ([]encodedSpan, bool) {
	if embeddedContextMayContribute == nil {
		return appendProtectedBase64Span(spans, input, span, charge, strictContribution, matchContext, false, mayContribute, work, status)
	}

	decoded, readable := decodeProtectedBase64Span(input, span, charge, work, status)
	if !readable {
		return spans, false
	}

	direct, nested, nestedStatus := matchingDecodedContributionDetails(decoded, mayContribute)
	mergeDecodeStatus(status, nestedStatus)

	if !decodeWorkComplete(status) {
		return spans, true
	}

	contributes := mode == embeddedDirectOrContext && direct
	if !contributes {
		contributes = embeddedContextContributes(input, span, decoded, nested, embeddedContextMayContribute, work, status)
	}

	if !contributes {
		return spans, true
	}

	if len(spans) == 0 || spans[len(spans)-1] != span {
		spans = append(spans, span)
	}

	return spans, true
}

type decodeChargePolicy uint8

const (
	chargePerAttempt decodeChargePolicy = iota
	chargeReadableOnly
)

func protectedHexSpans(input string, mayContribute func(string) bool, work *protectedDecodeWork, status *DecodeStatus) []encodedSpan {
	matches := shortHexPayloadPattern.FindAllStringSubmatchIndex(input, maxProtectedDecodeTries+1)
	spans := make([]encodedSpan, 0, min(len(matches), maxDecodeScans+1))

	for _, match := range matches {
		if len(match) != 4 {
			continue
		}

		span := encodedSpan{start: match[2], end: match[3]}
		decoded, err := decodeHexPayload(input[span.start:span.end])

		if err != nil || !IsReadableText(decoded) {
			continue
		}

		if !consumeProtectedDecodeWork(work, status, span.end-span.start) {
			break
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

func contextualWindowContribution(window string, mayContribute func(string) bool) (bool, DecodeStatus) {
	return decodedContribution(window, mayContribute, false)
}

func matchingDecodedContributionDetails(decoded string, mayContribute func(string) bool) (bool, DecodeResult, DecodeStatus) {
	if mayContribute == nil || mayContribute(decoded) {
		return true, DecodeResult{}, 0
	}

	nested := DecodeCandidates(decoded)
	if !nested.Complete() {
		return false, nested, nested.Status
	}

	if slices.ContainsFunc(nested.Candidates, mayContribute) {
		return true, nested, 0
	}

	if nested.maxDepth >= maxDecodeDepth {
		return false, nested, DecodeDepthLimit
	}

	if shortNestedDecodeMayContribute(decoded, mayContribute) {
		return true, nested, 0
	}

	if shortResult, ok := decodeSingleShortRuleContext(decoded, mayContribute); ok {
		if !shortResult.Complete() {
			return false, nested, shortResult.Status
		}

		if len(shortResult.Candidates) > 0 {
			return true, nested, 0
		}
	}

	return false, nested, 0
}

func matchingContextualDecodedContribution(
	input string,
	span encodedSpan,
	decoded string,
	nested DecodeResult,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
) (bool, DecodeStatus) {
	if mayContribute == nil {
		return true, 0
	}

	if mayContribute(contextualMatchSurface(input, span, decoded)) {
		return true, 0
	}

	var status DecodeStatus

	for _, candidate := range nested.Candidates {
		surface := contextualMatchSurface(input, span, candidate)
		if !consumeProtectedContextWork(work, &status, len(surface)) {
			return false, status
		}

		if mayContribute(surface) {
			return true, status
		}
	}

	return false, status
}

func contextualMatchSurface(input string, span encodedSpan, decoded string) string {
	if window, bounded := contextualAdmissionCandidate(input, span, decoded); bounded {
		return window
	}

	return replaceDecodedSpan(input, span, decoded)
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
