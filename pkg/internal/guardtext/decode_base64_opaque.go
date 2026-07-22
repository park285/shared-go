package guardtext

import (
	"bytes"
	"strings"
)

const maxOpaqueBase64ProbeChars = 64

func isOpaqueBase64Envelope(input string, span encodedSpan) bool {
	if span.start < 0 || span.start >= span.end || span.end > len(input) {
		return false
	}

	value := input[span.start:span.end]
	if len(value)%4 == 1 && len(value) > 4 {
		if decoded, ok := decodeBase64Probe(value[1:]); ok && hasOpaqueBinarySignature(decoded) {
			return true
		}
	}

	decoded, ok := decodeBase64Probe(value)
	if !ok {
		return false
	}
	if hasOpaqueBinarySignature(decoded) {
		return true
	}

	return hasOpaqueDataURIContext(input, span.start) && !IsReadableText(decoded)
}

func decodeBase64Probe(value string) ([]byte, bool) {
	probeBytes := min(len(value), maxOpaqueBase64ProbeChars)
	probeBytes -= probeBytes % 4
	if probeBytes < 4 {
		return nil, false
	}

	decoded, err := DecodeBase64Candidate(value[:probeBytes])
	return decoded, err == nil
}

func hasOpaqueDataURIContext(input string, payloadStart int) bool {
	if payloadStart <= 0 || input[payloadStart-1] != ',' {
		return false
	}

	windowStart := max(0, payloadStart-256)
	metadata := strings.ToLower(input[windowStart : payloadStart-1])
	dataStart := strings.LastIndex(metadata, "data:")
	if dataStart < 0 {
		return false
	}

	parts := strings.Split(metadata[dataStart+len("data:"):], ";")
	if len(parts) < 2 || !slicesContainASCIIFold(parts[1:], "base64") {
		return false
	}

	return isOpaqueDataMediaType(strings.TrimSpace(parts[0]))
}

func slicesContainASCIIFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}

	return false
}

func isOpaqueDataMediaType(mediaType string) bool {
	if mediaType == "" || strings.HasPrefix(mediaType, "text/") {
		return false
	}
	if strings.Contains(mediaType, "json") || strings.Contains(mediaType, "xml") ||
		strings.Contains(mediaType, "javascript") || strings.Contains(mediaType, "ecmascript") ||
		strings.Contains(mediaType, "yaml") || strings.Contains(mediaType, "toml") ||
		mediaType == "application/x-www-form-urlencoded" || mediaType == "image/svg+xml" {
		return false
	}

	return true
}

func hasOpaqueBinarySignature(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) ||
		bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) ||
		bytes.HasPrefix(data, []byte("GIF87a")) ||
		bytes.HasPrefix(data, []byte("GIF89a")) ||
		bytes.HasPrefix(data, []byte("%PDF-")) ||
		bytes.HasPrefix(data, []byte{'P', 'K', 0x03, 0x04}) ||
		bytes.HasPrefix(data, []byte{'P', 'K', 0x05, 0x06}) ||
		bytes.HasPrefix(data, []byte{'P', 'K', 0x07, 0x08}) ||
		bytes.HasPrefix(data, []byte{0x1f, 0x8b}) ||
		bytes.HasPrefix(data, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}) ||
		bytes.HasPrefix(data, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}) ||
		bytes.HasPrefix(data, []byte("Rar!\x1a\x07")) ||
		isWebP(data) || isZlib(data)
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
}

func isZlib(data []byte) bool {
	if len(data) < 2 || data[0]&0x0f != 8 || data[0]>>4 > 7 {
		return false
	}

	return (int(data[0])<<8|int(data[1]))%31 == 0
}
