package guardtext

import "strings"

// DecodeCandidatesWithContext expands supported encodings while retaining the
// surrounding text. This prevents a protected phrase from being split between
// plaintext and an encoded fragment that would otherwise be evaluated as two
// unrelated surfaces.
func DecodeCandidatesWithContext(input string) DecodeResult {
	if !hasPotentialDecodeSurface(input) {
		return DecodeResult{}
	}

	result := DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)}
	queue := []decodeQueueEntry{{text: input}}
	visited := map[string]struct{}{input: {}}
	total, scans := 0, 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, candidate := range decodeContextSurfaces(current.text, &scans, &result.Status) {
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

func decodeContextSurfaces(input string, scans *int, status *DecodeStatus) []string {
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
			span := family.spans[family.next]
			*scans++
			decoded, ok := family.attempt()
			if !ok {
				continue
			}
			switch family.kind {
			case decodeBase64:
				values = append(values, decoded, replaceDecodedSpan(input, span, decoded))
			case decodeHex:
				span.start = contextualHexStart(input, span.start)
				values = append(values, decoded, replaceDecodedSpan(input, span, decoded))
			default:
				values = append(values, decoded)
			}
		}
	}
	return values
}

func replaceDecodedSpan(input string, span encodedSpan, decoded string) string {
	if span.start < 0 || span.start > span.end || span.end > len(input) {
		return input
	}
	var output strings.Builder
	output.Grow(len(input) - (span.end - span.start) + len(decoded))
	output.WriteString(input[:span.start])
	output.WriteString(decoded)
	output.WriteString(input[span.end:])
	return output.String()
}

func contextualHexStart(input string, payloadStart int) int {
	position := payloadStart
	for position > 0 && isASCIIContextSpace(input[position-1]) {
		position--
	}
	if position == 0 || input[position-1] != ':' {
		return payloadStart
	}
	position--
	for position > 0 && isASCIIContextSpace(input[position-1]) {
		position--
	}
	if position < len("hex") || !strings.EqualFold(input[position-len("hex"):position], "hex") {
		return payloadStart
	}
	return position - len("hex")
}

func isASCIIContextSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}
