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

	return decodeCandidatesWithContext(input, false, nil, nil)
}

// DecodeCandidatesWithContextForProtected는 protected text에 기여할 수 있는 짧은 Base64·hex 조각만 bounded BFS에 추가한다.
func DecodeCandidatesWithContextForProtected(
	input string,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
) DecodeResult {
	semantic := decodeSemanticRuleInput(input, mayContribute)
	if semantic.status != 0 {
		return DecodeResult{Status: semantic.status}
	}
	decoded := decodeCandidatesWithContext(semantic.projected, true, mayContribute, embeddedContextMayContribute)

	return mergeSemanticCandidates(semantic.candidates, decoded)
}

// EmbeddedContextMatcher는 모호한 Base64 내부 경계를 디코딩한 값이 원래 문맥에서도 기여하는지 판정한다.
type EmbeddedContextMatcher func(input string, encodedStart, encodedEnd int, decoded string) bool

func decodeCandidatesWithContext(
	input string,
	includeShort bool,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
) DecodeResult {
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
		result:                       DecodeResult{Candidates: make([]string, 0, maxDecodeCandidates)},
		queue:                        make([]decodeQueueEntry, 0, len(roots)+maxDecodeCandidates),
		visited:                      make(map[string]struct{}, len(roots)+maxDecodeCandidates),
		includeShort:                 includeShort,
		embeddedContextMayContribute: embeddedContextMayContribute,
		seenWholes:                   newSpanContextSeen(input),
		roots:                        roots,
	}
	decoder.mayContribute = mayContribute
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
	result                       DecodeResult
	queue                        []decodeQueueEntry
	visited                      map[string]struct{}
	cursor                       int
	total                        int
	scans                        int
	includeShort                 bool
	mayContribute                func(string) bool
	embeddedContextMayContribute EmbeddedContextMatcher
	oversizedWouldBlock          func(string, string, []string) bool
	protectedWork                protectedDecodeWork
	seenWholes                   *spanContextSeen
	roots                        []string
}

type decodeContextOptions struct {
	includeShort           bool
	filterCandidates       bool
	boundOversizedStandard bool
}

func (d *contextDecoder) pending() bool {
	return d.cursor < len(d.queue)
}

func (d *contextDecoder) expandNext() {
	current := d.queue[d.cursor]
	d.cursor++
	options := decodeContextOptions{
		includeShort:           d.includeShort,
		boundOversizedStandard: d.includeShort,
	}
	decodeContextSurfaces(current.text, options, d.seenWholes, d.mayContribute, d.embeddedContextMayContribute, d.oversizedWouldBlock, &d.protectedWork, &d.scans, &d.result.Status, nil, func(candidate string) {
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

// 규칙·protected 매칭은 splice 경계 국소이되 구분자 run이 projection에서 접히므로,
// 승격 후보는 collapse-인식 경계 윈도우 splice로 충분하다. 반복 본문에서는 동일
// 윈도우가 visited dedup으로 접혀 후보 한도를 위치 변형이 소진하지 않는다. 전체
// splice를 승격하면 큰 정상 답변에서 크기·후보 한도가 소진되어 fail-closed 오차단이 된다.
func (d *contextDecoder) admitContextual(current decodeQueueEntry, span encodedSpan, decoded string) {
	contextual, bounded := contextualAdmissionCandidate(current.text, span, decoded)
	if !bounded {
		d.result.Status |= DecodeByteLimit
		return
	}
	candidateBytes := len(contextual)
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
	d.admit(current, contextual)
}

func decodeContextSurfaces(
	input string,
	options decodeContextOptions,
	seenWholes *spanContextSeen,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	oversizedWouldBlock func(string, string, []string) bool,
	work *protectedDecodeWork,
	scans *int,
	status *DecodeStatus,
	observe func(decodedContextCandidate),
	admit func(string),
	admitContextual func(encodedSpan, string),
) {
	families := transformFamiliesWithShortContext(input, options.includeShort, seenWholes, mayContribute, embeddedContextMayContribute, work, status)
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
			candidate, ok := decodeContextCandidate(
				input,
				family,
				options,
				mayContribute,
				embeddedContextMayContribute,
				oversizedWouldBlock,
				work,
				status,
			)
			if !ok {
				continue
			}
			if observe != nil {
				observe(candidate)
			}
			admitDecodedContextCandidate(candidate, options, admit, admitContextual)
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
	options decodeContextOptions,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
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
	if options.boundOversizedStandard && len(decoded) > maxDecodedCandidateLen && isWholeContextTransform(family.kind) {
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

	candidate := decodedContextCandidate{kind: family.kind, span: span, decoded: decoded}
	classifyCandidateContribution(input, &candidate, options, mayContribute, embeddedContextMayContribute, work, status)

	return candidate, true
}

func classifyCandidateContribution(
	input string,
	candidate *decodedContextCandidate,
	options decodeContextOptions,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) {
	var nested DecodeResult
	var nestedStatus DecodeStatus
	if options.filterCandidates {
		candidate.decodedMayContribute, nested, nestedStatus = matchingDecodedContributionDetails(candidate.decoded, mayContribute)
	} else {
		candidate.decodedMayContribute, nestedStatus = protectedDecodedContribution(candidate.decoded, mayContribute)
	}
	mergeDecodeStatus(status, nestedStatus)
	hasSurroundingContext := candidate.hasSurroundingContext(len(input))
	if hasSurroundingContext && !options.includeShort && !options.filterCandidates {
		candidate.contextMayContribute = true
	} else if hasSurroundingContext && options.filterCandidates && !candidate.decodedMayContribute {
		if embeddedContextMayContribute != nil {
			classifyEmbeddedContextCandidate(input, candidate, nested, embeddedContextMayContribute)

			return
		}
		contextBytes := len(input) - (candidate.span.end - candidate.span.start) + len(candidate.decoded)
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return
		}
		var bounded bool
		candidate.contextual, bounded = contextualAdmissionCandidate(input, candidate.span, candidate.decoded)
		if !bounded {
			*status |= DecodeByteLimit

			return
		}
		candidate.contextMayContribute, nestedStatus = matchingContextualDecodedContribution(input, candidate.span, candidate.decoded, nested, mayContribute, work)
		mergeDecodeStatus(status, nestedStatus)
	} else if hasSurroundingContext && candidate.decodedMayContribute {
		contextBytes := len(input) - (candidate.span.end - candidate.span.start) + len(candidate.decoded)
		if !consumeProtectedContextWork(work, status, contextBytes) {
			return
		}
		var bounded bool
		candidate.contextual, bounded = contextualAdmissionCandidate(input, candidate.span, candidate.decoded)
		if !bounded {
			*status |= DecodeByteLimit

			return
		}
		candidate.contextMayContribute, nestedStatus = contextualWindowContribution(candidate.contextual, mayContribute)
		mergeDecodeStatus(status, nestedStatus)
	}
}

func classifyEmbeddedContextCandidate(
	input string,
	candidate *decodedContextCandidate,
	nested DecodeResult,
	matcher EmbeddedContextMatcher,
) {
	candidate.contextMayContribute = embeddedContextContributes(input, candidate.span, candidate.decoded, nested, matcher)
	if !candidate.contextMayContribute {
		return
	}
	contextual, bounded := contextualAdmissionCandidate(input, candidate.span, candidate.decoded)
	if !bounded {
		candidate.contextMayContribute = false

		return
	}
	candidate.contextual = contextual
}

func (c decodedContextCandidate) hasContext() bool {
	return c.kind == decodeBase64 || c.kind == decodeHex
}

func (c decodedContextCandidate) hasSurroundingContext(inputBytes int) bool {
	return c.hasContext() && (c.span.start > 0 || c.span.end < inputBytes)
}

func admitDecodedContextCandidate(candidate decodedContextCandidate, options decodeContextOptions, admit func(string), admitContextual func(encodedSpan, string)) {
	if candidate.boundedStandard {
		for _, bounded := range candidate.boundedCandidates {
			admit(bounded)
		}

		return
	}
	if options.filterCandidates {
		if candidate.decodedMayContribute {
			admit(candidate.decoded)
		}
		if candidate.contextMayContribute {
			admit(candidate.contextual)
		}

		return
	}
	if !options.includeShort || candidate.decodedMayContribute {
		admit(candidate.decoded)
	}
	if options.includeShort && candidate.contextMayContribute {
		admit(candidate.contextual)
	} else if candidate.hasContext() && !options.includeShort {
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
