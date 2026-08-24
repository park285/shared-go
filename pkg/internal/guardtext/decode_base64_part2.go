package guardtext

import (
	"encoding/base64"
	"errors"
	"unicode"
	"unicode/utf8"
)

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

func ContainsSuspiciousBase64(input string) bool {
	for i := 0; i < len(input); {
		start := i //nolint:copyloopvar // 루프 변수가 본문에서 전진하므로 시작 위치를 따로 보존한다.
		match := nextBase64Candidate(input, i)

		i = match.next

		if len(match.value) < minBase64CandidateLen {
			continue
		}

		if declaredNonTextDataPayload(input, start) {
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
			return destination[:decodedBytes], nil
		}

		lastErr = err
	}

	return nil, lastErr
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

// IsReadableText와 판정이 같되 []byte 변환 복사를 피한다.
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
