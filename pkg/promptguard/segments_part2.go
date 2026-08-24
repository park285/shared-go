package promptguard

import (
	"strings"
)

func (s *segmentizer) appendLine(line string, nextKind segmentKind) bool {
	if s.buffer.Len() == 0 {
		s.currentKind = nextKind
	} else if nextKind != s.currentKind {
		if !s.flush(s.currentKind) {
			return false
		}

		s.currentKind = nextKind
	}

	s.buffer.WriteString(line)

	return true
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

	var distinctKeys [3]string

	distinctKeyCount := 0
	hasRulepackKeys := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		total++

		if key, ok := configLineKey(line); ok {
			keyValue++

			distinctKeyCount = appendDistinctKey(distinctKeys[:], distinctKeyCount, key)
		}

		if strings.Contains(trimmed, "rules:") || strings.Contains(trimmed, "pattern:") || strings.Contains(trimmed, "weight:") {
			hasRulepackKeys = true
		}
	}

	if total == 0 {
		return false
	}

	if hasRulepackKeys {
		return true
	}

	if distinctKeyCount < len(distinctKeys) {
		return false
	}

	return keyValue >= 3 || float64(keyValue)/float64(total) >= 0.35
}

func appendDistinctKey(keys []string, count int, key string) int {
	if count >= len(keys) {
		return count
	}

	for _, existing := range keys[:count] {
		if strings.EqualFold(existing, key) {
			return count
		}
	}

	keys[count] = key

	return count + 1
}

func configLineKey(line string) (string, bool) {
	value := strings.TrimLeft(line, " \t\r\f\v")
	if strings.HasPrefix(value, "-") {
		rest := value[1:]

		value = strings.TrimLeft(rest, " \t\r\f\v")

		if len(value) == len(rest) {
			return "", false
		}
	}

	end := 0
	for end < len(value) && isConfigKeyByte(value[end]) {
		end++
	}

	if end == 0 {
		return "", false
	}

	rest := strings.TrimLeft(value[end:], " \t\r\f\v")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}

	return value[:end], true
}

func isConfigKeyByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '.' || value == '-'
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
