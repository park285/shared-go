package guardtext

import (
	"slices"
	"strings"
)

// DecodeCandidatesWithContext는 주변 평문을 보존한 채 지원 인코딩을 확장한다.
func DecodeCandidatesWithContext(input string) DecodeResult {
	standalone := DecodeCandidates(input)
	if !standalone.Complete() || standalone.standaloneBase64 && len(standalone.Candidates) > 0 {
		return standalone
	}

	return decodeCandidatesWithContext(input, false, false)
}

// DecodeCandidatesWithContextForProtected는 protected text에 기여할 수 있는 짧은 Base64·hex 조각만 bounded BFS에 추가한다.
func DecodeCandidatesWithContextForProtected(input string, mayContribute ...func(string) bool) DecodeResult {
	return decodeCandidatesWithContext(input, true, false, mayContribute...)
}

// DecodeCandidatesWithContextForMatching은 matcher에 기여할 수 있는 긴 인코딩 구간만 주변 문맥과 함께 확장한다.
func DecodeCandidatesWithContextForMatching(input string, mayContribute func(string) bool) DecodeResult {
	return decodeCandidatesWithContext(input, false, true, mayContribute)
}

func decodeCandidatesWithContext(input string, includeShort, filterLong bool, mayContribute ...func(string) bool) DecodeResult {
	roots := []string{input}
	if includeShort {
		normalized := NormalizeEncodingSyntax(input)
		if normalized != input {
			roots = append(roots, normalized)
		}
	}
	if !slices.ContainsFunc(roots, func(root string) bool {
		return hasPotentialContextDecodeSurface(root, includeShort)
	}) {
		return DecodeResult{}
	}

	decoder := contextDecoder{
		result:       DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:        make([]decodeQueueEntry, 0, len(roots)+maxDecodeCandidates),
		visited:      make(map[string]struct{}, len(roots)+maxDecodeCandidates),
		includeShort: includeShort,
		filterLong:   filterLong,
	}
	if len(mayContribute) > 0 {
		decoder.mayContribute = mayContribute[0]
	}
	for _, root := range roots {
		if _, exists := decoder.visited[root]; exists {
			continue
		}
		decoder.visited[root] = struct{}{}
		decoder.queue = append(decoder.queue, decodeQueueEntry{text: root})
	}
	for decoder.pending() {
		decoder.expandNext()
	}
	return decoder.result
}

func hasPotentialContextDecodeSurface(input string, includeShort bool) bool {
	if hasPotentialDecodeSurface(input) {
		return true
	}
	if !includeShort {
		return false
	}
	return hasPlausibleShortDecodeSurface(input)
}

type contextDecoder struct {
	result              DecodeResult
	queue               []decodeQueueEntry
	visited             map[string]struct{}
	cursor              int
	total               int
	scans               int
	includeShort        bool
	filterLong          bool
	mayContribute       func(string) bool
	oversizedWouldBlock func(string, string, []string) bool
	protectedWork       protectedDecodeWork
}

func (d *contextDecoder) pending() bool {
	return d.cursor < len(d.queue)
}

func (d *contextDecoder) expandNext() {
	current := d.queue[d.cursor]
	d.cursor++
	decodeContextSurfaces(current.text, d.includeShort, d.filterLong, d.includeShort, d.mayContribute, d.oversizedWouldBlock, &d.protectedWork, &d.scans, &d.result.Status, func(candidate string) {
		d.admit(current, candidate)
	}, func(span encodedSpan, decoded string) {
		d.admitContextual(current, span, decoded)
	})
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

func (d *contextDecoder) admitContextual(current decodeQueueEntry, span encodedSpan, decoded string) {
	candidateBytes := len(current.text) - (span.end - span.start) + len(decoded)
	if candidateBytes > maxDecodedCandidateLen || d.total+candidateBytes > maxDecodedTotalBytes {
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
	d.admit(current, replaceDecodedSpan(current.text, span, decoded))
}

func decodeContextSurfaces(
	input string,
	includeShort bool,
	filterLong bool,
	boundOversizedStandard bool,
	mayContribute func(string) bool,
	oversizedWouldBlock func(string, string, []string) bool,
	work *protectedDecodeWork,
	scans *int,
	status *DecodeStatus,
	admit func(string),
	admitContextual func(encodedSpan, string),
) {
	families := transformFamiliesWithShortContext(input, includeShort, filterLong, mayContribute, work, status)
	if !decodeWorkComplete(status) {
		return
	}
	for familiesPending(families) {
		for i := range families {
			family := &families[i]
			if family.next >= len(family.spans) {
				continue
			}
			if !consumeContextDecodeScan(scans, status) {
				return
			}
			candidate, ok := decodeContextCandidate(input, family, includeShort, boundOversizedStandard, mayContribute, oversizedWouldBlock, work, status)
			if !ok {
				continue
			}
			admitDecodedContextCandidate(candidate, includeShort, admit, admitContextual)
		}
	}
}

type decodedContextCandidate struct {
	kind                 decodeFamily
	span                 encodedSpan
	decoded              string
	contextual           string
	boundedStandard      bool
	boundedCandidates    []string
	decodedMayContribute bool
	contextMayContribute bool
}

func consumeContextDecodeScan(scans *int, status *DecodeStatus) bool {
	if *scans >= maxDecodeScans {
		*status |= DecodeScanLimit

		return false
	}
	*scans++

	return true
}

func decodeContextCandidate(
	input string,
	family *transformFamily,
	includeShort bool,
	boundOversizedStandard bool,
	mayContribute func(string) bool,
	oversizedWouldBlock func(string, string, []string) bool,
	work *protectedDecodeWork,
	status *DecodeStatus,
) (decodedContextCandidate, bool) {
	span := family.spans[family.next]
	decoded, ok := family.attempt()
	if !ok || !IsReadableText([]byte(decoded)) {
		return decodedContextCandidate{}, false
	}
	if family.kind == decodeHex {
		span.start = contextualHexStart(input, span.start)
	}
	if boundOversizedStandard && len(decoded) > maxDecodedCandidateLen && isWholeContextTransform(family.kind) {
		bounded, nestedStatus := boundedStandardTransformCandidates(input, family.kind, family.spans, mayContribute)
		mergeDecodeStatus(status, nestedStatus)
		if oversizedWouldBlock != nil && oversizedWouldBlock(input, decoded, bounded) {
			*status |= DecodeByteLimit
		}

		return decodedContextCandidate{
			kind:              family.kind,
			span:              span,
			boundedStandard:   true,
			boundedCandidates: bounded,
		}, true
	}

	decodedMayContribute, nestedStatus := protectedDecodedContribution(decoded, mayContribute)
	mergeDecodeStatus(status, nestedStatus)
	candidate := decodedContextCandidate{kind: family.kind, span: span, decoded: decoded, decodedMayContribute: decodedMayContribute}
	if candidate.hasContext() && !includeShort {
		candidate.contextMayContribute = true
	} else if candidate.hasContext() && candidate.decodedMayContribute {
		contextBytes := len(input) - (span.end - span.start) + len(decoded)
		if contextBytes > maxDecodedCandidateLen {
			*status |= DecodeByteLimit

			return candidate, true
		}
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return candidate, true
		}
		candidate.contextual = replaceDecodedSpan(input, span, decoded)
		candidate.contextMayContribute, nestedStatus = protectedDecodedContribution(candidate.contextual, mayContribute)
		mergeDecodeStatus(status, nestedStatus)
	}

	return candidate, true
}

func (c decodedContextCandidate) hasContext() bool {
	return c.kind == decodeBase64 || c.kind == decodeHex
}

func admitDecodedContextCandidate(candidate decodedContextCandidate, includeShort bool, admit func(string), admitContextual func(encodedSpan, string)) {
	if candidate.boundedStandard {
		for _, bounded := range candidate.boundedCandidates {
			admit(bounded)
		}

		return
	}
	if !includeShort || candidate.decodedMayContribute {
		admit(candidate.decoded)
	}
	if includeShort && candidate.contextMayContribute {
		admit(candidate.contextual)
	} else if candidate.hasContext() && !includeShort {
		admitContextual(candidate.span, candidate.decoded)
	}
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
