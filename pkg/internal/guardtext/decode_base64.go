package guardtext

import (
	"encoding/base64"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

type base64Candidate struct {
	value string
	next  int
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
