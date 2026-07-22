package guardtext

import (
	"html"
	"strings"
)

const (
	maxDecodeCandidates      = 8
	minBase64CandidateLen    = 20
	maxDecodedCandidateLen   = 8 << 10
	maxDecodedTotalBytes     = 16 << 10
	maxDecodeDepth           = 2
	maxDecodeScans           = 64
	maxProtectedDecodeTries  = 4096
	maxProtectedDecodeBytes  = 4 << 20
	maxProtectedContextBytes = 16 << 20
	maxHTMLEntityNameBytes   = 31
	maxLegacyHTMLEntityBytes = 6
)

// DecodeStatus는 제한 안에서 완료하지 못한 decode 작업을 나타낸다.
type DecodeStatus uint8

const (
	DecodeCandidateLimit DecodeStatus = 1 << iota
	DecodeByteLimit
	DecodeDepthLimit
	DecodeScanLimit
)

type DecodeResult struct {
	Candidates       []string
	Status           DecodeStatus
	standaloneBase64 bool
	maxDepth         int
}

func (r DecodeResult) Complete() bool { return r.Status == 0 }

type decodeQueueEntry struct {
	text  string
	depth int
}

type encodedSpan struct{ start, end int }

type protectedDecodeWork struct {
	tries        int
	bytes        int
	contextBytes int
}

type decodeFamily uint8

const (
	decodeBase64 decodeFamily = iota
	decodePercent
	decodeHTML
	decodeJSON
	decodeHex
)

type transformFamily struct {
	kind  decodeFamily
	input string
	spans []encodedSpan
	next  int
}

// DecodeCandidates는 읽을 수 있는 지원 인코딩을 너비 우선으로 확장하고 미완료 원인을 Status에 남긴다.
func DecodeCandidates(input string) DecodeResult {
	potential, standaloneBase64 := classifyPotentialDecodeSurface(input)
	if !potential {
		return DecodeResult{}
	}
	result := DecodeResult{
		Candidates:       make([]string, 0, maxDecodeCandidates),
		standaloneBase64: standaloneBase64,
	}
	queue := []decodeQueueEntry{{text: input}}
	visited := map[string]struct{}{input: {}}
	total, scans := 0, 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, candidate := range decodeSurfaces(current.text, &scans, &result.Status) {
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
			candidateDepth := current.depth + 1
			result.maxDepth = max(result.maxDepth, candidateDepth)
			queue = append(queue, decodeQueueEntry{text: candidate, depth: candidateDepth})
		}
	}
	return result
}

func hasPotentialDecodeSurface(input string) bool {
	potential, _ := classifyPotentialDecodeSurface(input)

	return potential
}

func classifyPotentialDecodeSurface(input string) (bool, bool) {
	if strings.ContainsAny(input, `%&\`) || containsASCIIFold(input, "hex") {
		return true, false
	}
	for i := 0; i < len(input); {
		start := i
		match := nextBase64Candidate(input, i)
		i = match.next
		if len(match.value) >= minBase64CandidateLen && !declaredNonTextDataPayload(input, start) {
			return true, start == 0 && match.next == len(input)
		}
	}
	return false, false
}

func containsASCIIFold(input, target string) bool {
	for i := 0; i+len(target) <= len(input); i++ {
		matched := true
		for j := range len(target) {
			value := input[i+j]
			if value >= 'A' && value <= 'Z' {
				value += 'a' - 'A'
			}
			if value != target[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func decodeSurfaces(input string, scans *int, status *DecodeStatus) []string {
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
			*scans++
			if candidate, ok := family.attempt(); ok {
				values = append(values, candidate)
			}
		}
	}
	return values
}

func transformFamilies(input string) []transformFamily {
	return []transformFamily{
		{kind: decodeBase64, input: input, spans: base64SpansAtLeast(input, minBase64CandidateLen)},
		{kind: decodePercent, input: input, spans: percentSpans(input)},
		{kind: decodeHTML, input: input, spans: htmlEntitySpans(input)},
		{kind: decodeJSON, input: input, spans: jsonEscapeSpans(input)},
		{kind: decodeHex, input: input, spans: hexSpansForPattern(input, hexPayloadPattern)},
	}
}

func transformFamiliesWithShortContext(
	input string,
	includeShort bool,
	mayContribute func(string) bool,
	embeddedContextMayContribute EmbeddedContextMatcher,
	work *protectedDecodeWork,
	status *DecodeStatus,
) []transformFamily {
	base64Minimum := minBase64CandidateLen
	hexPattern := hexPayloadPattern
	base64Spans := contextualBase64SpansAtLeast(input, base64Minimum)
	hexSpans := hexSpansForPattern(input, hexPattern)
	if includeShort {
		base64Spans = protectedBase64Spans(input, mayContribute, embeddedContextMayContribute, work, status)
		hexSpans = protectedHexSpans(input, mayContribute, work, status)
	}
	families := []transformFamily{
		{kind: decodeBase64, input: input, spans: base64Spans},
		{kind: decodePercent, input: input, spans: percentSpans(input)},
		{kind: decodeHTML, input: input, spans: htmlEntitySpans(input)},
		{kind: decodeJSON, input: input, spans: jsonEscapeSpans(input)},
		{kind: decodeHex, input: input, spans: hexSpans},
	}
	return families
}

func familiesPending(families []transformFamily) bool {
	for i := range families {
		if families[i].next < len(families[i].spans) {
			return true
		}
	}
	return false
}

func (f *transformFamily) attempt() (string, bool) {
	span := f.spans[f.next]
	f.next++
	switch f.kind {
	case decodeBase64:
		decoded, err := DecodeBase64Candidate(f.input[span.start:span.end])
		return string(decoded), err == nil
	case decodePercent:
		if f.next != len(f.spans) {
			return "", false
		}
		return decodePercentRuns(f.input)
	case decodeHTML:
		if f.next != len(f.spans) {
			return "", false
		}
		decoded := html.UnescapeString(f.input)
		return decoded, decoded != f.input
	case decodeJSON:
		if f.next != len(f.spans) {
			return "", false
		}
		return decodeJSONStringEscapes(f.input)
	case decodeHex:
		decoded, err := decodeHexPayload(f.input[span.start:span.end])
		return string(decoded), err == nil
	default:
		return "", false
	}
}
