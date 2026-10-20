package guardtext

import (
	"html"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// RuleDecodeRegion은 원문의 한 연결 영역과 그 영역 전체를 치환하는 실제 해석이다.
// 부분 윈도우나 서로 다른 해석을 합성한 문자열은 Readings에 넣지 않는다.
type RuleDecodeRegion struct {
	Start, End int
	Readings   []string
}

// HasIndependentRuleRegions는 서로 떨어진 디코딩 영역이 둘 이상인지 할당 없이 판정한다.
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

// RuleDecodeInputs는 원문을 보존하면서 기존 문법 정규화·분할 Base64 해석을 더한다.
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

// RuleRegionDecoder는 입력 전체의 실제 변환 작업 예산을 공유한다. 각 영역을 바이트·작업
// 한도 안에서 끝까지 확장하며, 한 영역의 제한이 다음 영역의 검사를 중단하지 않는다.
type RuleRegionDecoder struct {
	work protectedDecodeWork
	// regions는 영역 텍스트별 해석이다. 해석은 영역 텍스트로만 정해지므로 반복 행과 원문·정규화
	// 입력에 같은 영역이 다시 나오면 해석을 반복하지 않고 위치만 바꿔 근거를 전달한다.
	regions map[string]regionExpansion
}

type regionExpansion struct {
	readings []string
	observed []string
	status   DecodeStatus
}

// ReserveContext는 실제 근거 문자열을 구성하기 전에 요청 전체 문맥 바이트 예산을 예약한다.
func (decoder *RuleRegionDecoder) ReserveContext(bytes int) DecodeStatus {
	var status DecodeStatus

	consumeProtectedContextWork(&decoder.work, &status, bytes)

	return status
}

// Walk는 독립 영역을 순서대로 검사한다. Evidence는 단독 디코딩 근거를 즉시 소비한다.
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

// base64는 hex: 표지가 붙은 값을 Base64로 다시 읽지 않는다. 표지 없는 원문 root가 같은 숫자열의 Base64
// 해석을 맡고, hex 숫자만인 Base64는 공백이 없고 위치마다 쓸 수 있는 글자도 제한돼 지시 문장을 만들지 못한다.
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

// decodedBase64는 스팬 수집이 이미 풀어 메모에 남긴 값을 다시 풀지 않는다. 메모의 빈 문자열은 읽을 수 없는 값이다.
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

// withoutHexPayloads는 hex: 표지 뒤 값을 같은 길이의 공백으로 바꿔 다른 해석의 탐색에서 뺀다. 값 뒤에
// Base64 문자가 이어지면 hex 값이 아니라 더 긴 토큰의 앞부분이므로, 가려서 Base64 정렬을 바꾸지 않는다.
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

// RuleRegionContext는 다른 영역과 정규화 또는 인코딩 경계가 결합되는 해석의 문맥을 반환한다.
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

// ruleJSONEscapeEnd는 영역을 잇는 JSON 이스케이프의 끝이다. 줄바꿈·탭 이스케이프는 풀면 공백이라
// 영역을 나누므로 ruleRegionGap이 건너뛴다.
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

// ruleRegionGap은 영역 사이 구분자 하나의 byte 길이다. JSON 줄바꿈·탭 이스케이프는 풀면 공백이므로
// 두 글자를 한 구분자로 건너뛴다. 뒤 글자를 다음 영역의 시작으로 읽으면 이어지는 값이 깨진다.
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
