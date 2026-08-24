package promptguard

import (
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

type segmentizer struct {
	segments    []textSegment
	buffer      strings.Builder
	currentKind segmentKind
	fenceMarker string
	limit       int
	inFence     bool
}

func segmentizeTextBounded(text string, limit int) ([]textSegment, bool) {
	state := segmentizer{
		segments:    make([]textSegment, 0, 8),
		currentKind: segmentPlain,
		limit:       limit,
	}

	for line := range strings.Lines(text) {
		nextKind, marker, changedFence := nextSegmentState(strings.TrimSpace(line), state.inFence, state.fenceMarker)

		switch {
		case changedFence:
			if !state.applyFenceChange(line, marker) {
				return nil, true
			}
		case state.inFence:
			state.buffer.WriteString(line)
		default:
			if !state.appendLine(line, nextKind) {
				return nil, true
			}
		}
	}

	if !state.flush(state.currentKind) {
		return nil, true
	}

	return state.segments, false
}

func (s *segmentizer) flush(kind segmentKind) bool {
	if s.buffer.Len() == 0 {
		return true
	}

	content := s.buffer.String()
	s.buffer.Reset()

	finalKind := segmentKindFromContent(kind, content)

	var exceeded bool

	s.segments, exceeded = appendSegmentContentBounded(s.segments, finalKind, content, s.limit)

	return !exceeded
}

func (s *segmentizer) applyFenceChange(line, marker string) bool {
	if s.inFence && marker == s.fenceMarker {
		s.buffer.WriteString(line)

		if !s.flush(segmentCode) {
			return false
		}

		s.inFence = false
		s.fenceMarker = ""
		s.currentKind = segmentPlain

		return true
	}

	if !s.flush(s.currentKind) {
		return false
	}

	s.inFence = true
	s.fenceMarker = marker
	s.currentKind = segmentCode

	s.buffer.WriteString(line)

	return true
}
