package guardtext

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

type base64Candidate struct {
	value string
	next  int
}

func base64SpansAtLeast(input string, minimum int) []encodedSpan {
	var (
		spans      []encodedSpan
		seenValues map[string]struct{}
		scratch    []byte
	)

	dedup := len(input) >= spanContextDedupMinInputBytes

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minimum || declaredNonTextDataPayload(input, start) {
			continue
		}

		if dedup {
			if _, duplicate := seenValues[match.value]; duplicate {
				continue
			}

			if seenValues == nil {
				seenValues = make(map[string]struct{}, 8)
			}

			seenValues[match.value] = struct{}{}
		}

		scratch = growBase64Scratch(scratch, match.value)

		decoded, err := decodeBase64CandidateInto(scratch, match.value)

		if err != nil || !IsReadableText(decoded) {
			continue
		}

		spans = append(spans, encodedSpan{start: start, end: match.next})
	}

	return spans
}

func growBase64Scratch(scratch []byte, encoded string) []byte {
	needed := base64.RawStdEncoding.DecodedLen(len(encoded))
	if cap(scratch) >= needed {
		return scratch[:needed]
	}

	return make([]byte, needed)
}

type spanContextRef struct {
	text string
	span encodedSpan
}

type spanContextSeen struct {
	entries map[string][]spanContextRef
}

const spanContextDedupMinInputBytes = 1 << 10

func newSpanContextSeen(input string) *spanContextSeen {
	if len(input) < spanContextDedupMinInputBytes {
		return nil
	}

	return &spanContextSeen{}
}

func (s *spanContextSeen) duplicate(text string, span encodedSpan) bool {
	if s == nil {
		return false
	}

	value := text[span.start:span.end]
	for _, prior := range s.entries[value] {
		if sameSpanContext(prior, spanContextRef{text: text, span: span}) {
			return true
		}
	}

	if s.entries == nil {
		s.entries = make(map[string][]spanContextRef, 8)
	}

	s.entries[value] = append(s.entries[value], spanContextRef{text: text, span: span})

	return false
}

func sameSpanContext(left, right spanContextRef) bool {
	const contextBytes = 512

	return left.text[max(0, left.span.start-contextBytes):left.span.start] ==
		right.text[max(0, right.span.start-contextBytes):right.span.start] &&
		left.text[left.span.end:min(len(left.text), left.span.end+contextBytes)] ==
			right.text[right.span.end:min(len(right.text), right.span.end+contextBytes)]
}

func contextualBase64SpansAtLeast(input string, minimum int) []encodedSpan {
	spans := make([]encodedSpan, 0, min(maxDecodeScans+1, len(input)/minimum))
	work := protectedDecodeWork{}
	seenWholes := newSpanContextSeen(input)

	for i := 0; i < len(input) && len(spans) <= maxDecodeScans; {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minimum {
			continue
		}

		whole := encodedSpan{start: start, end: match.next}
		if pathSegments, ok := httpURLPathBase64Segments(input, whole, minimum); ok {
			for _, segment := range pathSegments {
				var complete bool

				spans, complete = appendContextualBase64Whole(spans, input, segment, minimum, &work, seenWholes)

				if !complete {
					return appendDecodeScanOverflow(spans, segment)
				}
			}

			continue
		}

		var complete bool

		spans, complete = appendContextualBase64Whole(spans, input, whole, minimum, &work, seenWholes)

		if !complete {
			return appendDecodeScanOverflow(spans, whole)
		}
	}

	return spans
}

func appendContextualBase64Whole(
	spans []encodedSpan,
	input string,
	whole encodedSpan,
	minimum int,
	work *protectedDecodeWork,
	seenWholes *spanContextSeen,
) ([]encodedSpan, bool) {
	if declaredNonTextDataPayload(input, whole.start) {
		return spans, true
	}

	if seenWholes.duplicate(input, whole) {
		return spans, true
	}

	value := input[whole.start:whole.end]
	decoded, err := DecodeBase64Candidate(value)

	if err == nil && IsReadableText(decoded) {
		return append(spans, whole), true
	}

	if !looksLikeEmbeddedBase64(value) {
		return spans, true
	}

	return appendReadableBase64Subspans(append(spans, whole), input, whole, minimum, work)
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

var (
	errBase64WordSeparators = errors.New("base64 decode: lowercase word decodes to separators only")
	errBase64Number         = errors.New("base64 decode: number token")
)

func base64Reading(input string, decoded []byte) ([]byte, error) {
	if strings.TrimLeft(input, "0123456789") == "" {
		return nil, errBase64Number
	}

	for index := range len(input) {
		if input[index] < 'a' || input[index] > 'z' {
			return decoded, nil
		}
	}

	for _, r := range string(decoded) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return decoded, nil
		}
	}

	return nil, errBase64WordSeparators
}

func DecodeBase64Candidate(input string) ([]byte, error) {
	if input == "" {
		return nil, errors.New("base64 decode: empty input")
	}

	var lastErr error

	for _, encoding := range candidateBase64Encodings(input) {
		decoded, err := encoding.DecodeString(input)
		if err == nil {
			return base64Reading(input, decoded)
		}

		lastErr = err
	}

	return nil, lastErr
}

var (
	strictBase64Std    = base64.StdEncoding.Strict()
	strictBase64URL    = base64.URLEncoding.Strict()
	strictBase64RawStd = base64.RawStdEncoding.Strict()
	strictBase64RawURL = base64.RawURLEncoding.Strict()
	base64PaddedURL    = [...]*base64.Encoding{strictBase64URL, strictBase64Std}
	base64PaddedStd    = [...]*base64.Encoding{strictBase64Std, strictBase64URL}
	base64RawURL       = [...]*base64.Encoding{strictBase64RawURL, strictBase64RawStd, strictBase64URL, strictBase64Std}
	base64RawStd       = [...]*base64.Encoding{strictBase64RawStd, strictBase64Std, strictBase64RawURL, strictBase64URL}
)

func decodeBase64CandidateInto(destination []byte, input string) ([]byte, error) {
	if input == "" {
		return nil, errors.New("base64 decode: empty input")
	}

	source := []byte(input)

	var lastErr error

	for _, encoding := range candidateBase64Encodings(input) {
		decodedBytes, err := encoding.Decode(destination, source)
		if err == nil {
			return base64Reading(input, destination[:decodedBytes])
		}

		lastErr = err
	}

	return nil, lastErr
}

func base64UnreadablePrefix(input string, work *protectedDecodeWork, status *DecodeStatus) (int, bool) {
	if len(input) < 4 {
		return 0, true
	}

	var storage [48]byte

	decoded := storage[:0]
	consumed, position, unreadable := 0, 0, 0

	for consumed+4 <= len(input) && unreadable == 0 {
		value, ok := base64Group(input[consumed : consumed+4])
		if !ok {
			break
		}

		var group [4]byte

		binary.BigEndian.PutUint32(group[:], value)

		decoded = append(decoded, group[1:]...)

		consumed += 4

		for position < len(decoded) && utf8.FullRune(decoded[position:]) {
			r, size := utf8.DecodeRune(decoded[position:])
			if r == utf8.RuneError && size == 1 {
				unreadable = len(decoded)

				break
			}

			position += size
		}
	}

	if consumed > 0 && !work.seenBase64Prefix(input[:consumed]) {
		if !consumeProtectedDecodeWork(work, status, consumed) {
			return 0, false
		}

		work.rememberBase64Prefix(input[:consumed])
	}

	return unreadable, true
}

func (work *protectedDecodeWork) seenBase64Prefix(prefix string) bool {
	if work == nil || work.base64PrefixMemo == nil {
		return false
	}

	_, exists := work.base64PrefixMemo[prefix]

	return exists
}

func (work *protectedDecodeWork) rememberBase64Prefix(prefix string) {
	if work != nil && work.base64PrefixMemo != nil {
		work.base64PrefixMemo[prefix] = struct{}{}
	}
}

func base64AlphabetEnd(input string, whole encodedSpan) int {
	end := whole.end
	for end > whole.start && input[end-1] == '=' {
		end--
	}

	return end
}

func base64PrefixExcludes(unreadable, alphabetLen int) bool {
	return unreadable > 0 && 3*(alphabetLen/4) >= unreadable
}

func base64UnexcludedEnd(unreadable, start, alphabetEnd int) int {
	covering := 4 * ((unreadable + 2) / 3)
	if unreadable <= 0 || alphabetEnd-start < covering {
		return math.MaxInt
	}

	return start + covering - 1
}

func base64Digit(char byte) (uint32, bool) {
	switch {
	case char >= 'A' && char <= 'Z':
		return uint32(char - 'A'), true
	case char >= 'a' && char <= 'z':
		return uint32(char-'a') + 26, true
	case char >= '0' && char <= '9':
		return uint32(char-'0') + 52, true
	case char == '+' || char == '-':
		return 62, true
	case char == '/' || char == '_':
		return 63, true
	default:
		return 0, false
	}
}

func base64Group(group string) (uint32, bool) {
	var value uint32

	for index := range len(group) {
		digit, ok := base64Digit(group[index])
		if !ok {
			return 0, false
		}

		value = value<<6 | digit
	}

	return value, true
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

func IsReadableString(data string) bool {
	if data == "" {
		return false
	}

	printable := 0
	total := 0

	for index := 0; index < len(data); {
		r, size := utf8.DecodeRuneInString(data[index:])
		if r == utf8.RuneError && size == 1 {
			return false
		}

		index += size
		total++

		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}

	return total > 0 && printable*100 > total*90
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
	hasPadding := false
	hasURLAlphabet := false
	hasStandardAlphabet := false

	for i := range len(input) {
		switch input[i] {
		case '=':
			hasPadding = true
		case '-', '_':
			hasURLAlphabet = true
		case '+', '/':
			hasStandardAlphabet = true
		}
	}

	if hasPadding {
		if hasURLAlphabet && !hasStandardAlphabet {
			return base64PaddedURL[:]
		}

		return base64PaddedStd[:]
	}

	if hasURLAlphabet && !hasStandardAlphabet {
		return base64RawURL[:]
	}

	return base64RawStd[:]
}

func decodeProtectedBase64Span(
	input string,
	span encodedSpan,
	charge decodeChargePolicy,
	work *protectedDecodeWork,
	status *DecodeStatus,
) (decodedText string, readable bool) {
	if (span.end-span.start)%4 == 1 {
		return "", false
	}

	value := input[span.start:span.end]

	var memo map[string]string

	if work != nil {
		memo = work.base64Memo
	}

	if decoded, exists := memo[value]; exists {
		return decoded, decoded != ""
	}

	if charge == chargePerAttempt && !consumeProtectedDecodeWork(work, status, span.end-span.start) {
		return "", false
	}

	if charge == chargePerAttempt && memo != nil {
		defer func() { memo[value] = decodedText }()
	}

	decoded, err := DecodeBase64Candidate(input[span.start:span.end])
	if err != nil || !IsReadableText(decoded) {
		return "", false
	}

	if charge == chargeReadableOnly && !consumeProtectedDecodeWork(work, status, span.end-span.start) {
		return "", false
	}

	return string(decoded), true
}
