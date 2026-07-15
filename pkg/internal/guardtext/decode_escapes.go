package guardtext

import (
	"encoding/hex"
	"errors"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	hexPayloadPattern      = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*((?:[0-9a-f]{2}[\s,:-]+){3,}[0-9a-f]{2})(?:[^0-9a-f]|$)`)
	shortHexPayloadPattern = regexp.MustCompile(`(?i)(?:^|\b)hex\s*:\s*([0-9a-f]{2}(?:[\s,:-]+[0-9a-f]{2})*)(?:[^0-9a-f]|$)`)
)

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
