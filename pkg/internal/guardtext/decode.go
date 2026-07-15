package guardtext

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDecodeCandidates      = 8
	minBase64CandidateLen    = 20
	maxDecodedCandidateLen   = 8 << 10
	maxDecodedTotalBytes     = 16 << 10
	maxDecodeDepth           = 2
	maxDecodeScans           = 64
	maxProtectedDecodeTries  = 4096
	maxProtectedDecodeBytes  = 4 << 20
	maxProtectedContextBytes = 16 << 20
	maxHTMLEntityNameBytes   = 31
	maxLegacyHTMLEntityBytes = 6
)

// DecodeStatus는 제한 안에서 완료하지 못한 decode 작업을 나타낸다.
type DecodeStatus uint8

const (
	DecodeCandidateLimit DecodeStatus = 1 << iota
	DecodeByteLimit
	DecodeDepthLimit
	DecodeScanLimit
)

type DecodeResult struct {
	Candidates []string
	Status     DecodeStatus
}

func (r DecodeResult) Complete() bool { return r.Status == 0 }

type base64Candidate struct {
	value string
	next  int
}

var (
	hexPayloadPattern      = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*((?:[0-9a-f]{2}[\s,:-]+){3,}[0-9a-f]{2})(?:[^0-9a-f]|$)`)
	shortHexPayloadPattern = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*([0-9a-f]{2}(?:[\s,:-]+[0-9a-f]{2})*)(?:[^0-9a-f]|$)`)
)

type decodeQueueEntry struct {
	text  string
	depth int
}

type encodedSpan struct{ start, end int }

type protectedDecodeWork struct {
	tries               int
	bytes               int
	contextBytes        int
	supportedCandidates int
	supportedBytes      int
}

type decodeFamily uint8

const (
	decodeBase64 decodeFamily = iota
	decodePercent
	decodeHTML
	decodeJSON
	decodeHex
)

type transformFamily struct {
	kind  decodeFamily
	input string
	spans []encodedSpan
	next  int
}

// DecodeCandidates는 읽을 수 있는 지원 인코딩을 너비 우선으로 확장하고 미완료 원인을 Status에 남긴다.
func DecodeCandidates(input string) DecodeResult {
	if !hasPotentialDecodeSurface(input) {
		return DecodeResult{}
	}

	result := DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)}
	queue := []decodeQueueEntry{{text: input}}
	visited := map[string]struct{}{input: {}}
	total, scans := 0, 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, candidate := range decodeSurfaces(current.text, &scans, &result.Status) {
			if candidate == current.text {
				continue
			}
			if _, ok := visited[candidate]; ok {
				continue
			}
			visited[candidate] = struct{}{}
			data := []byte(candidate)
			if len(data) == 0 || !IsReadableText(data) {
				continue
			}
			if len(data) > maxDecodedCandidateLen || total+len(data) > maxDecodedTotalBytes {
				result.Status |= DecodeByteLimit
				continue
			}
			if current.depth >= maxDecodeDepth {
				result.Status |= DecodeDepthLimit
				continue
			}
			if len(result.Candidates) >= maxDecodeCandidates {
				result.Status |= DecodeCandidateLimit
				continue
			}
			result.Candidates = append(result.Candidates, candidate)
			total += len(data)
			queue = append(queue, decodeQueueEntry{text: candidate, depth: current.depth + 1})
		}
	}
	return result
}

func hasPotentialDecodeSurface(input string) bool {
	if strings.ContainsAny(input, `%&\`) || containsASCIIFold(input, "hex") {
		return true
	}
	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) >= minBase64CandidateLen {
			return true
		}
	}
	return false
}

func containsASCIIFold(input, target string) bool {
	for i := 0; i+len(target) <= len(input); i++ {
		matched := true
		for j := range len(target) {
			value := input[i+j]
			if value >= 'A' && value <= 'Z' {
				value += 'a' - 'A'
			}
			if value != target[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func decodeSurfaces(input string, scans *int, status *DecodeStatus) []string {
	values := make([]string, 0, 5)
	families := transformFamilies(input)
	for familiesPending(families) {
		for i := range families {
			family := &families[i]
			if family.next >= len(family.spans) {
				continue
			}
			if *scans >= maxDecodeScans {
				*status |= DecodeScanLimit
				return values
			}
			*scans++
			if candidate, ok := family.attempt(); ok {
				values = append(values, candidate)
			}
		}
	}
	return values
}

func transformFamilies(input string) []transformFamily {
	return []transformFamily{
		{kind: decodeBase64, input: input, spans: base64SpansAtLeast(input, minBase64CandidateLen)},
		{kind: decodePercent, input: input, spans: percentSpans(input)},
		{kind: decodeHTML, input: input, spans: htmlEntitySpans(input)},
		{kind: decodeJSON, input: input, spans: jsonEscapeSpans(input)},
		{kind: decodeHex, input: input, spans: hexSpansForPattern(input, hexPayloadPattern)},
	}
}

func transformFamiliesWithShortContext(
	input string,
	includeShort bool,
	filterLong bool,
	mayContribute func(string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []transformFamily {
	base64Minimum := minBase64CandidateLen
	hexPattern := hexPayloadPattern
	base64Spans := contextualBase64SpansAtLeast(input, base64Minimum)
	hexSpans := hexSpansForPattern(input, hexPattern)
	if includeShort {
		base64Spans = protectedBase64Spans(input, mayContribute, work, status)
		hexSpans = protectedHexSpans(input, mayContribute, work, status)
	} else if filterLong {
		base64Spans = matchingBase64Spans(input, mayContribute, work, status)
	}
	families := []transformFamily{
		{kind: decodeBase64, input: input, spans: base64Spans},
		{kind: decodePercent, input: input, spans: percentSpans(input)},
		{kind: decodeHTML, input: input, spans: htmlEntitySpans(input)},
		{kind: decodeJSON, input: input, spans: jsonEscapeSpans(input)},
		{kind: decodeHex, input: input, spans: hexSpans},
	}
	return families
}

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

func familiesPending(families []transformFamily) bool {
	for i := range families {
		if families[i].next < len(families[i].spans) {
			return true
		}
	}
	return false
}

func (f *transformFamily) attempt() (string, bool) {
	span := f.spans[f.next]
	f.next++
	switch f.kind {
	case decodeBase64:
		decoded, err := DecodeBase64Candidate(f.input[span.start:span.end])
		return string(decoded), err == nil
	case decodePercent:
		if f.next != len(f.spans) {
			return "", false
		}
		return decodePercentRuns(f.input)
	case decodeHTML:
		if f.next != len(f.spans) {
			return "", false
		}
		decoded := html.UnescapeString(f.input)
		return decoded, decoded != f.input
	case decodeJSON:
		if f.next != len(f.spans) {
			return "", false
		}
		return decodeJSONStringEscapes(f.input)
	case decodeHex:
		decoded, err := decodeHexPayload(f.input[span.start:span.end])
		return string(decoded), err == nil
	default:
		return "", false
	}
}

func base64SpansAtLeast(input string, minimum int) []encodedSpan {
	var spans []encodedSpan
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) >= minimum {
			spans = append(spans, encodedSpan{start: start, end: match.next})
		}
	}
	return spans
}

func contextualBase64SpansAtLeast(input string, minimum int) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/minimum))
	work := protectedDecodeWork{}
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < minimum {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		spans = append(spans, whole)
		decoded, err := DecodeBase64Candidate(match.value)
		if err == nil && IsReadableText(decoded) || !looksLikeEmbeddedBase64(match.value) {
			continue
		}

		var complete bool
		spans, complete = appendReadableBase64Subspans(spans, input, whole, minimum, &work)
		if !complete {
			return appendDecodeScanOverflow(spans, whole)
		}
	}

	return spans
}

func appendReadableBase64Subspans(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	work *protectedDecodeWork,
) ([]encodedSpan, bool) {
	maximumTrim := whole.end - whole.start - minimum
	for trimmed := 1; trimmed <= maximumTrim && len(spans) <= maxDecodeScans; trimmed++ {
		for leftTrim := 0; leftTrim <= trimmed && len(spans) <= maxDecodeScans; leftTrim++ {
			span := encodedSpan{
				start: whole.start + leftTrim,
				end:   whole.end - (trimmed - leftTrim),
			}
			var status DecodeStatus
			if !consumeProtectedDecodeWork(work, &status, span.end-span.start) {
				return spans, false
			}
			decoded, err := DecodeBase64Candidate(input[span.start:span.end])
			if err != nil || !IsReadableText(decoded) {
				continue
			}
			spans = append(spans, span)
		}
	}

	return spans, true
}

func appendDecodeScanOverflow(spans []encodedSpan, fallback encodedSpan) []encodedSpan {
	for len(spans) <= maxDecodeScans {
		spans = append(spans, fallback)
	}

	return spans
}

func percentSpans(input string) []encodedSpan {
	var spans []encodedSpan
	for i := 0; i+2 < len(input) && len(spans) <= maxDecodeScans; {
		if input[i] != '%' || !isHex(input[i+1]) || !isHex(input[i+2]) {
			i++
			continue
		}
		start := i
		for i+2 < len(input) && input[i] == '%' && isHex(input[i+1]) && isHex(input[i+2]) {
			i += 3
		}
		spans = append(spans, encodedSpan{start: start, end: i})
	}
	return spans
}

func htmlEntitySpans(input string) []encodedSpan {
	var spans []encodedSpan
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; i++ {
		if input[i] != '&' {
			continue
		}
		end, ok := supportedHTMLEntityEnd(input, i)
		if !ok {
			continue
		}
		spans = append(spans, encodedSpan{start: i, end: end})
		i = end - 1
	}
	return spans
}

func supportedHTMLEntityEnd(input string, start int) (int, bool) {
	if start+1 >= len(input) {
		return 0, false
	}
	if input[start+1] == '#' {
		return numericHTMLEntityEnd(input, start)
	}
	end := start + 1
	for end < len(input) && isASCIIAlphaNumeric(input[end]) {
		end++
	}
	if end == start+1 {
		return 0, false
	}
	if end < len(input) && input[end] == ';' && end-start-1 <= maxHTMLEntityNameBytes {
		candidate := input[start : end+1]
		if html.UnescapeString(candidate) != candidate {
			return end + 1, true
		}
	}
	legacyEnd := min(end, start+1+maxLegacyHTMLEntityBytes)
	candidate := input[start:legacyEnd]
	if html.UnescapeString(candidate) != candidate {
		return legacyEnd, true
	}
	return 0, false
}

func numericHTMLEntityEnd(input string, start int) (int, bool) {
	if len(input)-start <= 3 {
		return 0, false
	}
	position := start + 2
	hexadecimal := false
	if position < len(input) && (input[position] == 'x' || input[position] == 'X') {
		hexadecimal = true
		position++
	}
	digits := position
	for position < len(input) && (isASCIIDigit(input[position]) || hexadecimal && isHex(input[position])) {
		position++
	}
	if position == digits {
		return 0, false
	}
	if position < len(input) && input[position] == ';' {
		position++
	}
	return position, true
}

func jsonEscapeSpans(input string) []encodedSpan {
	var spans []encodedSpan
	for i := 0; i+1 < len(input) && len(spans) <= maxDecodeScans; i++ {
		if input[i] != '\\' {
			continue
		}
		end := i + 2
		switch input[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			if i+5 >= len(input) || !allHex(input[i+2:i+6]) {
				continue
			}
			end = i + 6
			if _, consumed, ok := decodeUnicodeEscape(input[i:]); ok {
				end = i + consumed
			}
		default:
			continue
		}
		spans = append(spans, encodedSpan{start: i, end: end})
		i = end - 1
	}
	return spans
}

func hexSpansForPattern(input string, pattern *regexp.Regexp) []encodedSpan {
	matches := pattern.FindAllStringSubmatchIndex(input, maxDecodeScans+1)
	spans := make([]encodedSpan, 0, len(matches))
	for _, match := range matches {
		if len(match) == 4 {
			spans = append(spans, encodedSpan{start: match[2], end: match[3]})
		}
	}
	return spans
}

func isASCIIAlphaNumeric(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }
func allHex(value string) bool {
	for i := range value {
		if !isHex(value[i]) {
			return false
		}
	}
	return true
}

func decodePercentRuns(input string) (string, bool) {
	var out strings.Builder
	changed := false
	for i := 0; i < len(input); {
		if i+2 >= len(input) || input[i] != '%' || !isHex(input[i+1]) || !isHex(input[i+2]) {
			out.WriteByte(input[i])
			i++
			continue
		}
		start := i
		var data []byte
		for i+2 < len(input) && input[i] == '%' && isHex(input[i+1]) && isHex(input[i+2]) {
			data = append(data, hexByte(input[i+1], input[i+2]))
			i += 3
		}
		if IsReadableText(data) {
			out.Write(data)
			changed = true
		} else {
			out.WriteString(input[start:i])
		}
	}
	return out.String(), changed
}

func isHex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }

func hexByte(high, low byte) byte { return hexNibble(high)<<4 | hexNibble(low) }

func hexNibble(b byte) byte {
	if b >= '0' && b <= '9' {
		return b - '0'
	}
	if b >= 'a' && b <= 'f' {
		return b - 'a' + 10
	}
	return b - 'A' + 10
}

func decodeJSONStringEscapes(input string) (string, bool) {
	if !strings.Contains(input, `\`) {
		return input, false
	}
	var out strings.Builder
	changed := false
	for i := 0; i < len(input); i++ {
		if input[i] != '\\' || i+1 >= len(input) {
			out.WriteByte(input[i])
			continue
		}
		escaped := input[i : i+2]
		switch input[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			out.WriteByte(simpleJSONEscape(input[i+1]))
			changed = true
			i++
		case 'u':
			if decoded, consumed, ok := decodeUnicodeEscape(input[i:]); ok {
				out.WriteString(decoded)
				changed = true
				i += consumed - 1
				continue
			}
			out.WriteString(escaped)
			i++
		default:
			out.WriteString(escaped)
			i++
		}
	}
	return out.String(), changed
}

func decodeUnicodeEscape(input string) (string, int, bool) {
	if len(input) < 6 {
		return "", 0, false
	}
	value, err := strconv.ParseInt(input[2:6], 16, 32)
	if err != nil {
		return "", 0, false
	}
	if value < 0xD800 || value > 0xDFFF {
		return string(rune(value)), 6, true
	}
	if value < 0xDC00 && len(input) >= 12 && input[6] == '\\' && input[7] == 'u' {
		low, lowErr := strconv.ParseInt(input[8:12], 16, 32)
		if lowErr == nil && low >= 0xDC00 && low <= 0xDFFF {
			return string(rune(0x10000 + (value-0xD800)<<10 + low - 0xDC00)), 12, true
		}
	}
	return "", 0, false
}

func simpleJSONEscape(escaped byte) byte {
	switch escaped {
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	default:
		return escaped
	}
}

func ContainsSuspiciousBase64(input string) bool {
	for i := 0; i < len(input); {
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) < minBase64CandidateLen {
			continue
		}
		decoded, err := DecodeBase64Candidate(match.value)
		if err == nil && IsReadableText(decoded) {
			return true
		}
	}

	return false
}

func DecodeBase64Candidate(input string) ([]byte, error) {
	if input == "" {
		return nil, errors.New("base64 decode: empty input")
	}

	var lastErr error
	for _, encoding := range candidateBase64Encodings(input) {
		decoded, err := encoding.DecodeString(input)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("base64 decode: %w", lastErr)
}

func IsReadableText(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	printable := 0
	total := 0
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		data = data[size:]
		total++
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}

	return total > 0 && printable*100 > total*90
}

func decodeHexPayload(input string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == ',' || r == ':' || r == '-' {
			return -1
		}

		return r
	}, input)
	if len(cleaned)%2 != 0 {
		return nil, errors.New("hex decode: odd payload length")
	}

	return hex.DecodeString(cleaned)
}

func nextBase64Candidate(input string, start int) base64Candidate {
	if !isBase64Char(input[start]) {
		return base64Candidate{next: start + 1}
	}

	next := start
	for next < len(input) && isBase64Char(input[next]) {
		next++
	}
	for padding := 0; next < len(input) && input[next] == '=' && padding < 2; padding++ {
		next++
	}

	return base64Candidate{value: input[start:next], next: next}
}

func isBase64Char(char byte) bool {
	return char >= 'A' && char <= 'Z' ||
		char >= 'a' && char <= 'z' ||
		char >= '0' && char <= '9' ||
		char == '+' || char == '/' || char == '-' || char == '_'
}

func candidateBase64Encodings(input string) []*base64.Encoding {
	hasPadding := strings.ContainsRune(input, '=')
	hasURLAlphabet := strings.ContainsAny(input, "-_")
	hasStandardAlphabet := strings.ContainsAny(input, "+/")

	if hasPadding {
		if hasURLAlphabet && !hasStandardAlphabet {
			return []*base64.Encoding{base64.URLEncoding.Strict(), base64.StdEncoding.Strict()}
		}

		return []*base64.Encoding{base64.StdEncoding.Strict(), base64.URLEncoding.Strict()}
	}
	if hasURLAlphabet && !hasStandardAlphabet {
		return []*base64.Encoding{
			base64.RawURLEncoding.Strict(),
			base64.RawStdEncoding.Strict(),
			base64.URLEncoding.Strict(),
			base64.StdEncoding.Strict(),
		}
	}

	return []*base64.Encoding{
		base64.RawStdEncoding.Strict(),
		base64.StdEncoding.Strict(),
		base64.RawURLEncoding.Strict(),
		base64.URLEncoding.Strict(),
	}
}
