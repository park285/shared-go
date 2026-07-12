package promptguard

import (
	"regexp"
	"strings"
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

	aggregates := make([]textSegment, 0, max(0, boundaries))
	for i := range boundaries {
		left := segments[i].Views
		right := segments[i+1].Views
		aggregates = append(aggregates, textSegment{
			Kind:  segmentPlain,
			Kinds: distinctSegmentKinds(segments[i].Kind, segments[i+1].Kind),
			Views: Views{
				Raw:    lastRunes(left.Raw, boundaryWindowRunes) + guardBoundaryMarker + firstRunes(right.Raw, boundaryWindowRunes),
				Norm:   lastRunes(left.Norm, boundaryWindowRunes) + guardBoundaryMarker + firstRunes(right.Norm, boundaryWindowRunes),
				Joined: lastRunes(left.Joined, boundaryWindowRunes) + guardBoundaryMarker + firstRunes(right.Joined, boundaryWindowRunes),
			},
			Aggregate: true,
		})
	}

	return append(segments, aggregates...), false
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
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit])
}

func lastRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[len(runes)-limit:])
}

func distinctSegmentKinds(values ...segmentKind) []segmentKind {
	seen := make(map[segmentKind]struct{}, len(values))
	result := make([]segmentKind, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
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
