package guardtext

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"regexp"
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
	maxHTMLEntityNameBytes   = 31
	maxLegacyHTMLEntityBytes = 6
)

// DecodeStatus records supported work that could not be evaluated under the
// bounded decoder limits. Callers must treat any non-zero value as incomplete.
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

var hexPayloadPattern = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*((?:[0-9a-f]{2}(?:[\s,:-]+|$)){4,})`)

type decodeQueueEntry struct {
	text  string
	depth int
}

type encodedSpan struct{ start, end int }

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

// DecodeCandidates breadth-first expands readable supported encodings. It never
// silently truncates a supported readable candidate: Status records the limit.
func DecodeCandidates(input string) DecodeResult {
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
	families := []transformFamily{
		{kind: decodeBase64, input: input, spans: base64Spans(input)},
		{kind: decodePercent, input: input, spans: percentSpans(input)},
		{kind: decodeHTML, input: input, spans: htmlEntitySpans(input)},
		{kind: decodeJSON, input: input, spans: jsonEscapeSpans(input)},
		{kind: decodeHex, input: input, spans: hexSpans(input)},
	}
	return families
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

func base64Spans(input string) []encodedSpan {
	var spans []encodedSpan
	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) >= minBase64CandidateLen {
			spans = append(spans, encodedSpan{start: start, end: match.next})
		}
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

func hexSpans(input string) []encodedSpan {
	matches := hexPayloadPattern.FindAllStringSubmatchIndex(input, maxDecodeScans+1)
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
