package promptguard

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type segmentKind string

type segmentKindSet uint8

const (
	segmentPlain  segmentKind = "plain"
	segmentQuote  segmentKind = "quote"
	segmentCode   segmentKind = "code"
	segmentConfig segmentKind = "config"
)

type textSegment struct {
	Kind      segmentKind
	Kinds     segmentKindSet
	Views     Views
	rawNorm   string
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
	if len(text) <= limit {
		return text
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
	if len(text) <= limit {
		return text
	}
	start := len(text)
	for range limit {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		if size == 0 {
			return text
		}
		start -= size
		if start == 0 {
			return text
		}
	}
	return text[start:]
}

func (kinds segmentKindSet) with(kind segmentKind) segmentKindSet {
	return kinds | segmentKindBit(kind)
}

func (kinds segmentKindSet) contains(kind segmentKind) bool {
	return kinds&segmentKindBit(kind) != 0
}

func segmentKindBit(kind segmentKind) segmentKindSet {
	switch kind {
	case segmentPlain:
		return 1 << 0
	case segmentQuote:
		return 1 << 1
	case segmentCode:
		return 1 << 2
	case segmentConfig:
		return 1 << 3
	default:
		return 0
	}
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
