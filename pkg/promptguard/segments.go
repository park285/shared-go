package promptguard

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type segmentKind string

const (
	segmentPlain  segmentKind = "plain"
	segmentQuote  segmentKind = "quote"
	segmentCode   segmentKind = "code"
	segmentConfig segmentKind = "config"
)

type textSegment struct {
	Kind      segmentKind
	Kinds     []segmentKind
	Views     Views
	RawNorm   string
	Aggregate bool
}

var configLinePattern = regexp.MustCompile(`^\s*(?:-\s+)?[A-Za-z0-9_.-]+\s*:`)

const (
	guardBoundaryMarker  = "\uE100"
	maxSegmentBoundaries = 256
	boundaryWindowRunes  = 256
)

func JoinParts(parts ...string) string {
	return strings.Join(parts, guardBoundaryMarker)
}

func buildEvaluationSegments(text string) ([]textSegment, bool) {
	const maxSegments = maxSegmentBoundaries + 1

	segments := make([]textSegment, 0, min(8, maxSegments))
	for part := range strings.SplitSeq(text, guardBoundaryMarker) {
		partSegments, exceeded := splitTextSegmentsBounded(part, maxSegments-len(segments))
		if exceeded {
			return nil, true
		}
		segments = append(segments, partSegments...)
	}
	boundaries := len(segments) - 1
	if boundaries <= 0 {
		return segments, false
	}

	aggregates := make([]textSegment, 0, boundaries)
	tail := aggregateTail{}
	tail.append(segments[0])
	for i := 1; i < len(segments); i++ {
		aggregates = append(aggregates, tail.aggregateWith(segments[i]))
		tail.append(segments[i])
	}

	return append(segments, aggregates...), false
}

type aggregateTail struct {
	Views  Views
	chunks []aggregateTailChunk
}

type aggregateTailChunk struct {
	Kind        segmentKind
	RawRunes    int
	NormRunes   int
	JoinedRunes int
}

func (t *aggregateTail) aggregateWith(right textSegment) textSegment {
	raw := t.Views.Raw + firstRunes(right.Views.Raw, boundaryWindowRunes)
	return textSegment{
		Kind:    segmentPlain,
		Kinds:   t.kindsWith(right.Kind),
		RawNorm: normalizeViews(raw).Norm,
		Views: Views{
			Raw:    raw,
			Norm:   t.Views.Norm + guardBoundaryMarker + firstRunes(right.Views.Norm, boundaryWindowRunes),
			Joined: t.Views.Joined + firstRunes(right.Views.Joined, boundaryWindowRunes),
		},
		Aggregate: true,
	}
}

func (t *aggregateTail) append(segment textSegment) {
	normSeparator := ""
	if len(t.chunks) > 0 {
		normSeparator = guardBoundaryMarker
	}

	t.chunks = append(t.chunks, aggregateTailChunk{
		Kind:        segment.Kind,
		RawRunes:    utf8.RuneCountInString(segment.Views.Raw),
		NormRunes:   utf8.RuneCountInString(normSeparator) + utf8.RuneCountInString(segment.Views.Norm),
		JoinedRunes: utf8.RuneCountInString(segment.Views.Joined),
	})

	var rawTrimmed, normTrimmed, joinedTrimmed int
	t.Views.Raw, rawTrimmed = appendAggregateTailView(t.Views.Raw, "", segment.Views.Raw)
	t.Views.Norm, normTrimmed = appendAggregateTailView(t.Views.Norm, normSeparator, segment.Views.Norm)
	t.Views.Joined, joinedTrimmed = appendAggregateTailView(t.Views.Joined, "", segment.Views.Joined)

	trimAggregateTailChunks(t.chunks, rawTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.RawRunes })
	trimAggregateTailChunks(t.chunks, normTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.NormRunes })
	trimAggregateTailChunks(t.chunks, joinedTrimmed, func(chunk *aggregateTailChunk) *int { return &chunk.JoinedRunes })
	t.compactChunks()
}

func appendAggregateTailView(current, separator, next string) (string, int) {
	currentRunes := utf8.RuneCountInString(current)
	separatorRunes := utf8.RuneCountInString(separator)
	nextRunes := utf8.RuneCountInString(next)
	total := currentRunes + separatorRunes + nextRunes
	if total <= boundaryWindowRunes {
		return current + separator + next, 0
	}

	keepCurrent := boundaryWindowRunes - separatorRunes - nextRunes
	if keepCurrent <= 0 {
		return lastRunes(separator+next, boundaryWindowRunes), total - boundaryWindowRunes
	}
	return lastRunes(current, keepCurrent) + separator + next, total - boundaryWindowRunes
}

func trimAggregateTailChunks(chunks []aggregateTailChunk, trim int, selectRunes func(*aggregateTailChunk) *int) {
	for i := range chunks {
		if trim <= 0 {
			return
		}
		value := selectRunes(&chunks[i])
		removed := min(*value, trim)
		*value -= removed
		trim -= removed
	}
}

func (t *aggregateTail) compactChunks() {
	kept := t.chunks[:0]
	for _, chunk := range t.chunks {
		if chunk.RawRunes == 0 && chunk.NormRunes == 0 && chunk.JoinedRunes == 0 {
			continue
		}
		kept = append(kept, chunk)
	}
	t.chunks = kept
}

func (t *aggregateTail) kindsWith(right segmentKind) []segmentKind {
	result := make([]segmentKind, 0, 4)
	for _, chunk := range t.chunks {
		result = appendDistinctSegmentKind(result, chunk.Kind)
	}
	return appendDistinctSegmentKind(result, right)
}

func splitTextSegments(text string) []textSegment {
	segments, exceeded := splitTextSegmentsBounded(text, maxSegmentBoundaries+1)
	if exceeded {
		return nil
	}

	return segments
}

func splitTextSegmentsBounded(text string, limit int) ([]textSegment, bool) {
	if limit <= 0 {
		return nil, true
	}

	raw := sanitizeUTF8(text)
	if strings.TrimSpace(raw) == "" {
		return []textSegment{{Kind: segmentPlain, Views: normalizeViews(raw)}}, false
	}

	segments, exceeded := segmentizeTextBounded(raw, limit)
	if exceeded {
		return nil, true
	}
	if len(segments) == 0 {
		return []textSegment{{Kind: segmentPlain, Views: normalizeViews(raw)}}, false
	}

	return segments, false
}

func firstRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}

func lastRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	total := utf8.RuneCountInString(text)
	if total <= limit {
		return text
	}

	skip := total - limit
	count := 0
	for index := range text {
		if count == skip {
			return text[index:]
		}
		count++
	}
	return ""
}

func distinctSegmentKinds(values ...segmentKind) []segmentKind {
	result := make([]segmentKind, 0, min(len(values), 4))
	for _, value := range values {
		result = appendDistinctSegmentKind(result, value)
	}
	return result
}

func appendDistinctSegmentKind(values []segmentKind, value segmentKind) []segmentKind {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func segmentizeTextBounded(text string, limit int) ([]textSegment, bool) {
	segments := make([]textSegment, 0, 8)

	var buffer strings.Builder

	currentKind := segmentPlain
	inFence := false
	fenceMarker := ""

	flush := func(kind segmentKind) bool {
		if buffer.Len() == 0 {
			return true
		}

		content := buffer.String()
		buffer.Reset()

		finalKind := segmentKindFromContent(kind, content)
		var exceeded bool
		segments, exceeded = appendSegmentContentBounded(segments, finalKind, content, limit)

		return !exceeded
	}

	for line := range strings.Lines(text) {
		nextKind, marker, changedFence := nextSegmentState(strings.TrimSpace(line), inFence, fenceMarker)
		if changedFence {
			if inFence && marker == fenceMarker {
				buffer.WriteString(line)
				if !flush(segmentCode) {
					return nil, true
				}
				inFence = false
				fenceMarker = ""
				currentKind = segmentPlain
			} else {
				if !flush(currentKind) {
					return nil, true
				}
				inFence = true
				fenceMarker = marker
				currentKind = segmentCode
				buffer.WriteString(line)
			}
			continue
		}

		if inFence {
			buffer.WriteString(line)

			continue
		}

		if buffer.Len() == 0 {
			currentKind = nextKind
		} else if nextKind != currentKind {
			if !flush(currentKind) {
				return nil, true
			}
			currentKind = nextKind
		}
		buffer.WriteString(line)
	}

	if !flush(currentKind) {
		return nil, true
	}

	return segments, false
}

func appendSegmentContentBounded(segments []textSegment, kind segmentKind, content string, limit int) ([]textSegment, bool) {
	if kind != segmentPlain || !strings.Contains(content, "`") {
		if len(segments) >= limit {
			return nil, true
		}

		return append(segments, textSegment{Kind: kind, Views: normalizeViews(content)}), false
	}

	initialLength := len(segments)
	partIndex := 0
	for part := range strings.SplitSeq(content, "`") {
		if part != "" {
			if len(segments) >= limit {
				return nil, true
			}
			segments = append(segments, newInlineSegment(part, partIndex))
		}
		partIndex++
	}
	if len(segments) == initialLength {
		if len(segments) >= limit {
			return nil, true
		}
		segments = append(segments, textSegment{Kind: kind, Views: normalizeViews(content)})
	}

	return segments, false
}

func nextSegmentState(trimmed string, inFence bool, fenceMarker string) (segmentKind, string, bool) {
	if marker, ok := fenceBoundary(trimmed); ok {
		if !inFence || marker == fenceMarker {
			return segmentCode, marker, true
		}
	}

	return classifyLine(trimmed), "", false
}

func segmentKindFromContent(kind segmentKind, content string) segmentKind {
	if kind == segmentPlain && looksLikeConfig(content) {
		return segmentConfig
	}

	return kind
}

func classifyLine(line string) segmentKind {
	trimmedLeft := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmedLeft, ">") {
		return segmentQuote
	}

	return segmentPlain
}

func fenceBoundary(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, "```"):
		return "```", true
	case strings.HasPrefix(line, "~~~"):
		return "~~~", true
	default:
		return "", false
	}
}

func looksLikeConfig(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return false
	}

	total := 0
	keyValue := 0
	hasRulepackKeys := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		total++

		if configLinePattern.MatchString(line) {
			keyValue++
		}

		if strings.Contains(trimmed, "rules:") || strings.Contains(trimmed, "pattern:") || strings.Contains(trimmed, "weight:") {
			hasRulepackKeys = true
		}
	}

	if total == 0 {
		return false
	}

	return hasRulepackKeys || keyValue >= 3 || float64(keyValue)/float64(total) >= 0.35
}

func newInlineSegment(part string, index int) textSegment {
	kind := segmentKindForInlinePart(part, index)

	return textSegment{
		Kind:  kind,
		Views: normalizeViews(part),
	}
}

func segmentKindForInlinePart(part string, index int) segmentKind {
	if index%2 == 1 {
		return segmentCode
	}

	if looksLikeConfig(part) {
		return segmentConfig
	}

	return segmentPlain
}
