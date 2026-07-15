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

	decoder := contextDecoder{
		result:  DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:   []decodeQueueEntry{{text: input}},
		visited: map[string]struct{}{input: {}},
	}
	for decoder.pending() {
		decoder.expandNext()
	}
	return decoder.result
}

type contextDecoder struct {
	result  DecodeResult
	queue   []decodeQueueEntry
	visited map[string]struct{}
	cursor  int
	total   int
	scans   int
}

func (d *contextDecoder) pending() bool {
	return d.cursor < len(d.queue)
}

func (d *contextDecoder) expandNext() {
	current := d.queue[d.cursor]
	d.cursor++
	candidates := decodeContextSurfaces(current.text, &d.scans, &d.result.Status)
	for _, candidate := range candidates {
		d.admit(current, candidate)
	}
}

func (d *contextDecoder) admit(current decodeQueueEntry, candidate string) {
	if candidate == current.text {
		return
	}
	if _, ok := d.visited[candidate]; ok {
		return
	}
	d.visited[candidate] = struct{}{}
	data := []byte(candidate)
	if len(data) == 0 || !IsReadableText(data) {
		return
	}
	if len(data) > maxDecodedCandidateLen || d.total+len(data) > maxDecodedTotalBytes {
		d.result.Status |= DecodeByteLimit
		return
	}
	if current.depth >= maxDecodeDepth {
		d.result.Status |= DecodeDepthLimit
		return
	}
	if len(d.result.Candidates) >= maxDecodeCandidates {
		d.result.Status |= DecodeCandidateLimit
		return
	}
	d.result.Candidates = append(d.result.Candidates, candidate)
	d.total += len(data)
	d.queue = append(d.queue, decodeQueueEntry{text: candidate, depth: current.depth + 1})
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
