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
	Kind  segmentKind
	Views Views
}

var configLinePattern = regexp.MustCompile(`^\s*(?:-\s+)?[A-Za-z0-9_.-]+\s*:`)

func splitTextSegments(text string) []textSegment {
	raw := sanitizeUTF8(text)
	if strings.TrimSpace(raw) == "" {
		return []textSegment{{Kind: segmentPlain, Views: normalizeViews(raw)}}
	}

	segments := segmentizeLines(strings.SplitAfter(raw, "\n"))
	if len(segments) == 0 {
		return []textSegment{{Kind: segmentPlain, Views: normalizeViews(raw)}}
	}

	return splitInlineCodeSegments(segments)
}

func segmentizeLines(lines []string) []textSegment {
	segments := make([]textSegment, 0, 8)

	var buffer strings.Builder

	currentKind := segmentPlain
	inFence := false
	fenceMarker := ""

	flush := func(kind segmentKind) {
		if buffer.Len() == 0 {
			return
		}

		content := buffer.String()
		buffer.Reset()

		finalKind := segmentKindFromContent(kind, content)

		segments = append(segments, textSegment{
			Kind:  finalKind,
			Views: normalizeViews(content),
		})
	}

	for _, line := range lines {
		nextKind, marker, changedFence := nextSegmentState(strings.TrimSpace(line), inFence, fenceMarker)
		if handleFenceTransition(&buffer, flush, line, marker, changedFence, &inFence, &fenceMarker, &currentKind) {
			continue
		}

		if inFence {
			buffer.WriteString(line)

			continue
		}

		currentKind = advanceSegmentKind(&buffer, flush, currentKind, nextKind)
		buffer.WriteString(line)
	}

	flush(currentKind)

	return segments
}

func handleFenceTransition(
	buffer *strings.Builder,
	flush func(segmentKind),
	line string,
	marker string,
	changedFence bool,
	inFence *bool,
	fenceMarker *string,
	currentKind *segmentKind,
) bool {
	if !changedFence {
		return false
	}

	if *inFence && marker == *fenceMarker {
		buffer.WriteString(line)
		flush(segmentCode)

		*inFence = false
		*fenceMarker = ""
		*currentKind = segmentPlain

		return true
	}

	flush(*currentKind)

	*inFence = true
	*fenceMarker = marker
	*currentKind = segmentCode

	buffer.WriteString(line)

	return true
}

func advanceSegmentKind(buffer *strings.Builder, flush func(segmentKind), currentKind, nextKind segmentKind) segmentKind {
	if buffer.Len() == 0 {
		return nextKind
	}

	if nextKind != currentKind {
		flush(currentKind)

		return nextKind
	}

	return currentKind
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

func splitInlineCodeSegments(segments []textSegment) []textSegment {
	out := make([]textSegment, 0, len(segments))
	for _, segment := range segments {
		out = append(out, expandInlineCodeSegment(segment)...)
	}

	if len(out) == 0 {
		return segments
	}

	return out
}

func expandInlineCodeSegment(segment textSegment) []textSegment {
	if segment.Kind != segmentPlain || !strings.Contains(segment.Views.Raw, "`") {
		return []textSegment{segment}
	}

	parts := strings.Split(segment.Views.Raw, "`")

	out := make([]textSegment, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}

		out = append(out, newInlineSegment(part, i))
	}

	return out
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
