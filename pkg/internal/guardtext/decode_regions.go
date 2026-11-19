package guardtext

import (
	"html"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type RuleDecodeRegion struct {
	Start, End int
	Readings   []string
}

func HasIndependentRuleRegions(input string) bool {
	count := 0

	for position := 0; position < len(input); {
		end := ruleRegionEnd(input, position)
		if end == position {
			position += ruleRegionGap(input, position)

			continue
		}

		if HasPotentialRuleDecodeSurface(input[position:end]) {
			count++
			if count == 2 {
				return true
			}
		}

		position = end
	}

	return false
}

func RuleDecodeInputs(input string) []string {
	inputs := []string{input}
	if normalized := NormalizeEncodingSyntax(input); normalized != input {
		inputs = append(inputs, normalized)
	}

	for _, value := range slices.Clone(inputs) {
		if joined, changed := normalizeSingleSpaceBase64(value); changed && !slices.Contains(inputs, joined) {
			inputs = append(inputs, joined)
		}
	}

	return inputs
}

type RuleRegionDecoder struct {
	work    protectedDecodeWork
	regions map[string]regionExpansion
}

type regionExpansion struct {
	readings []string
	observed []string
	status   DecodeStatus
}

func (decoder *RuleRegionDecoder) ReserveContext(bytes int) DecodeStatus {
	var status DecodeStatus

	consumeProtectedContextWork(&decoder.work, &status, bytes)

	return status
}

func (decoder *RuleRegionDecoder) Walk(input string, evidence func(int, int, string), visit func(RuleDecodeRegion)) DecodeStatus {
	var status DecodeStatus

	decoder.work.regionInteriors = true

	for start := 0; start < len(input); {
		end := ruleRegionEnd(input, start)
		if end == start {
			start += ruleRegionGap(input, start)

			continue
		}

		expansion := decoder.expandRegion(input[start:end])
		for _, value := range expansion.observed {
			evidence(start, end, value)
		}

		status |= expansion.status
		if len(expansion.readings) > 0 {
			visit(RuleDecodeRegion{Start: start, End: end, Readings: expansion.readings})
		}

		start = end
	}

	return status
}

func (decoder *RuleRegionDecoder) expandRegion(text string) regionExpansion {
	if expansion, exists := decoder.regions[text]; exists {
		return expansion
	}

	var expansion regionExpansion

	if HasPotentialRuleDecodeSurface(text) {
		if decoder.work.base64Memo == nil {
			decoder.work.base64Memo = make(map[string]string)
			decoder.work.base64PrefixMemo = make(map[string]struct{})
		}

		region := regionDecoder{
			work:    &decoder.work,
			visited: map[string]struct{}{text: {}},
			queue:   []string{text},
			observe: func(value string) { expansion.observed = append(expansion.observed, value) },
		}
		region.expand()

		expansion.readings, expansion.status = region.readings, region.status
	}

	if decoder.regions == nil {
		decoder.regions = make(map[string]regionExpansion)
	}

	decoder.regions[text] = expansion

	return expansion
}

type regionDecoder struct {
	work     *protectedDecodeWork
	visited  map[string]struct{}
	queue    []string
	readings []string
	observe  func(string)
	status   DecodeStatus
}

func (decoder *regionDecoder) expand() {
	cursor := 0
	for cursor < len(decoder.queue) {
		current := decoder.queue[cursor]
		cursor++

		decoder.standard(current)
		decoder.base64(current)
		decoder.hex(current)
	}
}

func (decoder *regionDecoder) standard(current string) {
	if strings.ContainsRune(current, '%') {
		if decoded, changed := decodePercentRuns(current); changed {
			decoder.admit(current, decoded, decoded)
		}
	}

	if decoded := html.UnescapeString(current); decoded != current {
		decoder.admit(current, decoded, decoded)
	}

	if decoded, changed := decodeJSONStringEscapes(current); changed {
		decoder.admit(current, decoded, decoded)
	}
}

func (decoder *regionDecoder) base64(current string) {
	searched := withoutHexPayloads(current)
	seen := newSpanContextSeen(searched)
	spans := ruleBase64Spans(searched, seen, everyRuleReading, nil, decoder.work, &decoder.status)

	short := shortRuleBase64Spans(searched, seen, everyRuleReading, nil, decoder.work, &decoder.status)
	if len(spans) > maxDecodeScans || len(short) > maxDecodeScans {
		decoder.status |= DecodeScanLimit
	}

	spans = append(spans, short...)

	for _, span := range spans {
		if value, readable := decoder.decodedBase64(current[span.start:span.end]); readable {
			decoder.admitSpan(current, span, value)
		}
	}
}

func (decoder *regionDecoder) decodedBase64(encoded string) (string, bool) {
	if value, exists := decoder.work.base64Memo[encoded]; exists {
		return value, value != ""
	}

	decoded, err := DecodeBase64Candidate(encoded)
	if err != nil || !IsReadableText(decoded) {
		return "", false
	}

	return string(decoded), true
}

func withoutHexPayloads(text string) string {
	if !containsHexFold(text) {
		return text
	}

	spans := hexSpansForPattern(text, shortHexPayloadPattern)
	if len(spans) == 0 {
		return text
	}

	masked := []byte(text)

	for _, span := range spans {
		if span.end < len(text) && isBase64Char(text[span.end]) {
			continue
		}

		for index := span.start; index < span.end; index++ {
			masked[index] = ' '
		}
	}

	return string(masked)
}

func (decoder *regionDecoder) hex(current string) {
	spans := hexSpansForPattern(current, shortHexPayloadPattern)
	if len(spans) > maxDecodeScans {
		decoder.status |= DecodeScanLimit
	}

	for _, span := range spans {
		if !consumeProtectedDecodeWork(decoder.work, &decoder.status, span.end-span.start) {
			return
		}

		decoded, err := decodeHexPayload(current[span.start:span.end])
		if err != nil || !IsReadableText(decoded) {
			continue
		}

		span.start = contextualHexStart(current, span.start)
		decoder.admitSpan(current, span, string(decoded))
	}
}

func (decoder *regionDecoder) admitSpan(current string, span encodedSpan, value string) {
	bytes := len(current) - (span.end - span.start) + len(value)
	if !consumeProtectedContextWork(decoder.work, &decoder.status, bytes) {
		return
	}

	decoder.admit(current, replaceDecodedSpan(current, span, value), value)
}

func (decoder *regionDecoder) admit(current, candidate, standalone string) {
	if candidate == current {
		return
	}

	if _, exists := decoder.visited[candidate]; exists {
		return
	}

	decoder.visited[candidate] = struct{}{}
	if !IsReadableString(candidate) {
		return
	}

	if !consumeProtectedContextWork(decoder.work, &decoder.status, len(candidate)+len(standalone)) {
		return
	}

	decoder.observe(standalone)

	if candidate != standalone {
		decoder.observe(candidate)
	}

	decoder.readings = append(decoder.readings, candidate)

	decoder.queue = append(decoder.queue, candidate)
}

func RuleRegionContext(input string, region RuleDecodeRegion, reading string) (string, bool) {
	unstable := !norm.NFKC.PropertiesString(reading).BoundaryBefore() ||
		region.End < len(input) && !norm.NFKC.PropertiesString(input[region.End:]).BoundaryBefore()
	if !unstable && !strings.ContainsAny(reading, `%&\`) && !containsHexFold(reading) {
		return "", true
	}

	context, replacement, bounded := contextualAdmissionDetails(input, encodedSpan{start: region.Start, end: region.End}, reading)
	if !bounded {
		return "", false
	}

	if unstable {
		return context, true
	}

	for _, span := range hexSpansForPattern(context, shortHexPayloadPattern) {
		span.start = contextualHexStart(context, span.start)
		if encodedSpanCrossesReplacementBoundary(span, replacement) {
			return context, true
		}
	}

	for _, spans := range [][]encodedSpan{percentSpans(context), htmlEntitySpans(context), jsonEscapeSpans(context)} {
		for _, span := range spans {
			if encodedSpanCrossesReplacementBoundary(span, replacement) {
				return context, true
			}
		}
	}

	return "", true
}

func ruleRegionEnd(input string, start int) int {
	end := start
	for end < len(input) {
		if next := hexRegionEnd(input, end); next > end {
			end = next
			continue
		}

		if isEncodedValueByte(input[end]) {
			end++
			continue
		}

		if input[end] == '%' && end+2 < len(input) && isHex(input[end+1]) && isHex(input[end+2]) {
			end += 3
			continue
		}

		if input[end] == '&' {
			if next, ok := supportedHTMLEntityEnd(input, end); ok {
				end = next
				continue
			}
		}

		if input[end] == '\\' {
			if next := ruleJSONEscapeEnd(input, end); next > end {
				end = next
				continue
			}
		}

		break
	}

	return end
}

func hexRegionEnd(input string, start int) int {
	if !hasASCIIFoldHexAt(input, start) || start > 0 && (isASCIIAlphaNumeric(input[start-1]) || input[start-1] == '_') {
		return start
	}

	end := start + 3
	for end < len(input) && isASCIIContextSpace(input[end]) {
		end++
	}

	if end == len(input) || input[end] != ':' {
		return start
	}

	end++
	for end < len(input) && (isHex(input[end]) || isASCIIContextSpace(input[end]) || strings.ContainsRune(",:-", rune(input[end]))) {
		end++
	}

	match := shortHexPayloadPattern.FindStringSubmatchIndex(input[start:min(len(input), end+1)])
	if len(match) != 4 || match[0] != 0 {
		return start
	}

	return start + match[3]
}

func ruleJSONEscapeEnd(input string, start int) int {
	if start+1 == len(input) {
		return start
	}

	switch input[start+1] {
	case '"', '\\', '/', 'b', 'f':
		return start + 2
	case 'u':
		if _, consumed, ok := decodeUnicodeEscape(input[start:]); ok {
			return start + consumed
		}
	}

	return start
}

func ruleRegionGap(input string, position int) int {
	if isJSONWhitespaceEscape(input, position) {
		return 2
	}

	_, size := utf8.DecodeRuneInString(input[position:])

	return size
}

func isJSONWhitespaceEscape(input string, position int) bool {
	return input[position] == '\\' && position+1 < len(input) && strings.IndexByte("nrt", input[position+1]) >= 0
}
