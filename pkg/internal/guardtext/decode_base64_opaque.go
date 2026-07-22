package guardtext

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"strings"
)

const (
	maxOpaqueBase64ProbeChars  = 64
	maxOpaqueDecodedProbeBytes = 48
	maxFactorioJSONProbeBytes  = 512
)

func projectOpaqueBase64ForRules(input string) string {
	var opaque []encodedSpan
	for position := 0; position < len(input); {
		start := position
		match := nextBase64Candidate(input, position)
		position = match.next
		if len(match.value) < minBase64CandidateLen {
			continue
		}
		span := encodedSpan{start: start, end: match.next}
		if isOpaqueBase64Envelope(input, span) {
			opaque = append(opaque, span)
		}
	}
	if len(opaque) == 0 {
		return input
	}

	var projected strings.Builder
	projected.Grow(len(input) - opaqueBytes(opaque) + len(opaque))
	position := 0
	for _, span := range opaque {
		projected.WriteString(input[position:span.start])
		projected.WriteByte(' ')
		position = span.end
	}
	projected.WriteString(input[position:])

	return projected.String()
}

func opaqueBytes(spans []encodedSpan) int {
	total := 0
	for _, span := range spans {
		total += span.end - span.start
	}

	return total
}

func isOpaqueBase64Envelope(input string, span encodedSpan) bool {
	if span.start < 0 || span.start >= span.end || span.end > len(input) {
		return false
	}

	value := input[span.start:span.end]
	if value[0] == '0' && len(value)%4 == 1 && len(value) > 4 && isFactorioBlueprintEnvelope(value[1:]) {
		return true
	}

	unreadable, opaqueSignature := classifyBase64Probe(value)
	if !unreadable {
		return false
	}
	if opaqueSignature {
		return true
	}

	return hasOpaqueDataURIContext(input, span.start)
}

func isFactorioBlueprintEnvelope(value string) bool {
	for _, encoding := range candidateBase64Encodings(value) {
		decoded := base64.NewDecoder(encoding, strings.NewReader(value))
		compressed, err := zlib.NewReader(decoded)
		if err != nil {
			continue
		}
		probe, readErr := io.ReadAll(io.LimitReader(compressed, maxFactorioJSONProbeBytes))
		closeErr := compressed.Close()
		if readErr != nil || closeErr != nil {
			continue
		}
		trimmed := bytes.TrimSpace(probe)
		if bytes.HasPrefix(trimmed, []byte(`{"blueprint":`)) || bytes.HasPrefix(trimmed, []byte(`{"blueprint_book":`)) {
			return true
		}
	}

	return false
}

func classifyBase64Probe(value string) (unreadable, opaqueSignature bool) {
	probeBytes := min(len(value), maxOpaqueBase64ProbeChars)
	probeBytes -= probeBytes % 4
	if probeBytes < 4 {
		return false, false
	}

	var buffer [maxOpaqueDecodedProbeBytes]byte
	decoded, err := decodeBase64CandidateInto(buffer[:], value[:probeBytes])
	if err != nil || IsReadableText(decoded) {
		return false, false
	}

	return true, hasOpaqueBinarySignature(decoded)
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
		isWebP(data)
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
}
