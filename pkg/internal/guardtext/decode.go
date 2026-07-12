package guardtext

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDecodeCandidates    = 8
	minBase64CandidateLen  = 20
	maxDecodedCandidateLen = 8 << 10
	maxDecodedTotalBytes   = 16 << 10
)

type base64Candidate struct {
	value string
	next  int
}

var (
	jsonUnicodePattern = regexp.MustCompile(`(?:\\u[0-9a-fA-F]{4})+`)
	hexPayloadPattern  = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*((?:[0-9a-f]{2}(?:[\s,:-]+|$)){4,})`)
)

type decodeCollector struct {
	input          string
	values         []string
	total          int
	base64Attempts int
}

func DecodeCandidates(input string) []string {
	collector := decodeCollector{
		input:  input,
		values: make([]string, 0, maxDecodeCandidates),
	}
	collector.collectBase64()
	collector.collectURL()
	collector.collectHTML()
	collector.collectJSONUnicode()
	collector.collectHex()

	return collector.values
}

func (c *decodeCollector) collectBase64() {
	for i := 0; i < len(c.input) && !c.full() && c.base64Attempts < maxDecodeCandidates; {
		match := nextBase64Candidate(c.input, i)
		i = match.next
		if len(match.value) < minBase64CandidateLen || len(match.value) > maxDecodedCandidateLen {
			continue
		}
		c.base64Attempts++
		decoded, err := DecodeBase64Candidate(match.value)
		if err == nil {
			c.consider(string(decoded))
		}
	}
}

func (c *decodeCollector) collectURL() {
	if c.full() || !strings.Contains(c.input, "%") {
		return
	}
	if decoded, err := url.PathUnescape(c.input); err == nil {
		c.consider(decoded)
	}
}

func (c *decodeCollector) collectHTML() {
	if !c.full() && strings.Contains(c.input, "&") {
		c.consider(html.UnescapeString(c.input))
	}
}

func (c *decodeCollector) collectJSONUnicode() {
	if !c.full() && strings.Contains(c.input, `\u`) {
		c.consider(decodeJSONUnicode(c.input))
	}
}

func (c *decodeCollector) collectHex() {
	if c.full() {
		return
	}
	if match := hexPayloadPattern.FindStringSubmatch(c.input); len(match) == 2 {
		if decoded, err := decodeHexPayload(match[1]); err == nil {
			c.consider(string(decoded))
		}
	}
}

func (c *decodeCollector) consider(candidate string) {
	if c.full() {
		return
	}
	if candidate == c.input {
		return
	}
	data := []byte(candidate)
	if len(data) == 0 || len(data) > maxDecodedCandidateLen || c.total+len(data) > maxDecodedTotalBytes || !IsReadableText(data) {
		return
	}
	c.values = append(c.values, candidate)
	c.total += len(data)
}

func (c *decodeCollector) full() bool {
	return len(c.values) >= maxDecodeCandidates || c.total >= maxDecodedTotalBytes
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

func decodeJSONUnicode(input string) string {
	return jsonUnicodePattern.ReplaceAllStringFunc(input, func(encoded string) string {
		decoded, err := strconv.Unquote(`"` + encoded + `"`)
		if err != nil {
			return encoded
		}

		return decoded
	})
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
