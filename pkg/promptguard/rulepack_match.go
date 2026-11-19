package promptguard

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

var allSegmentKinds = [...]segmentKind{segmentPlain, segmentQuote, segmentCode, segmentConfig}

func (r *compiledRule) appliesToSegment(kind segmentKind) bool {
	_, ok := r.Segments[kind]
	return ok
}

func (r *compiledRule) appliesToTextSegment(segment textSegment) bool {
	if !segment.Aggregate {
		return r.appliesToSegment(segment.Kind)
	}

	for _, kind := range allSegmentKinds {
		if segment.Kinds.contains(kind) && r.appliesToSegment(kind) {
			return true
		}
	}

	return false
}

func (r *compiledRule) masksText() bool {
	return len(r.Exclude) > 0 || r.SentenceBreak > 0
}

func (r *compiledRule) matchSegment(segment textSegment, policy compiledPolicy, limit int) []Match {
	if !r.masksText() {
		return r.matchSegmentText(segment, policy, limit)
	}

	if segment.Aggregate {
		return r.matchAggregateExcluding(segment, policy, limit)
	}

	matches := r.matchSegmentText(segment, policy, limit)
	if len(matches) == 0 {
		return nil
	}

	var (
		views   guardtext.Views
		changed bool
	)

	if segment.piece != nil {
		views, changed = segment.piece.maskedViews(r)
	} else {
		views, changed = r.maskViews(segment.Views)
	}

	if !changed {
		return matches
	}

	return r.matchSegmentText(textSegment{Kind: segment.Kind, Views: views}, policy, limit)
}

func (r *compiledRule) maskViews(views guardtext.Views) (guardtext.Views, bool) {
	masked, changed := r.maskExclusions(views.Raw)
	if !changed {
		return views, false
	}

	return normalizeViews(masked), true
}

const ruleExclusionMask = '\uE101'

var sentenceBoundaryPattern = regexp.MustCompile(`[^\s.!?。．！？]\s+[^\s.!?。．！？]+([.!?。．！？]\s+)`)

func (r *compiledRule) maskExclusions(raw string) (string, bool) {
	type maskSpan struct{ start, end, runes int }

	var spans []maskSpan

	for index, exclusion := range r.Exclude {
		if !containsAllLiteralGroupsASCIIFold(raw, r.ExcludeLiteralGroups[index]) {
			continue
		}

		if exclusion.NumSubexp() == 0 {
			for _, span := range exclusion.FindAllStringIndex(raw, -1) {
				spans = append(spans, maskSpan{start: span[0], end: span[1], runes: -1})
			}

			continue
		}

		for _, span := range exclusion.FindAllStringSubmatchIndex(raw, -1) {
			if span[2] >= 0 {
				spans = append(spans, maskSpan{start: span[2], end: span[3], runes: -1})
			}
		}
	}

	if r.SentenceBreak > 0 {
		for _, span := range sentenceBoundaryPattern.FindAllStringSubmatchIndex(raw, -1) {
			spans = append(spans, maskSpan{start: span[2], end: span[3], runes: r.SentenceBreak})
		}
	}

	if len(spans) == 0 {
		return raw, false
	}

	slices.SortFunc(spans, func(left, right maskSpan) int { return left.start - right.start })

	var masked strings.Builder

	masked.Grow(len(raw))

	position := 0

	for _, span := range spans {
		if span.end <= position {
			continue
		}

		start := max(span.start, position)
		masked.WriteString(raw[position:start])

		runes := span.runes
		if runes < 0 {
			runes = utf8.RuneCountInString(raw[start:span.end])
		}

		for range runes {
			masked.WriteRune(ruleExclusionMask)
		}

		position = span.end
	}

	masked.WriteString(raw[position:])

	return masked.String(), true
}

func (r *compiledRule) matchAggregateExcluding(segment textSegment, policy compiledPolicy, limit int) []Match {
	matches := r.crossingMatches(segment, policy, limit)
	if len(matches) == 0 {
		return nil
	}

	right, rightChanged := segment.piece.maskedViews(r)
	left, leftChanged := r.maskedAggregateLeft(segment.rawChunks)

	if !leftChanged && !rightChanged {
		return matches
	}

	rebuilt := textSegment{Kind: segment.Kind, Kinds: segment.Kinds, Aggregate: true}
	leftRunes := utf8.RuneCountInString(left)

	if r.View == viewAggregateNorm {
		rebuilt.Views.Norm = left + guardBoundaryMarker + right.Norm
		rebuilt.rawChunks = []aggregateTailChunk{{NormRunes: leftRunes}}
	} else {
		rebuilt.Views.Joined = left + right.Joined
		rebuilt.rawChunks = []aggregateTailChunk{{JoinedRunes: leftRunes}}
	}

	if len(r.crossingMatches(rebuilt, policy, 1)) > 0 {
		return matches
	}

	return nil
}

func (r *compiledRule) maskedAggregateLeft(chunks []aggregateTailChunk) (string, bool) {
	separator := ""

	if r.View == viewAggregateNorm {
		separator = guardBoundaryMarker
	}

	var parts []string

	changed, runes := false, 0

	for index := len(chunks) - 1; index >= 0 && runes <= r.MaxMatchRunes; index-- {
		views, pieceChanged := chunks[index].piece.maskedViews(r)
		text := segmentView(textSegment{Views: views}, r.View)

		parts = append(parts, text)
		changed = changed || pieceChanged

		runes += utf8.RuneCountInString(text) + utf8.RuneCountInString(separator)
	}

	slices.Reverse(parts)

	return strings.Join(parts, separator), changed
}

func (r *compiledRule) crossingMatches(segment textSegment, policy compiledPolicy, limit int) []Match {
	view, weight, ok := r.matchInput(segment, policy)
	if !ok {
		return nil
	}

	leftEnd, rightStart := segment.aggregateViewSplit(r.View, view)
	offset := byteOffsetBeforeRunes(view, leftEnd, r.MaxMatchRunes+1)
	text := view[offset : rightStart+byteOffsetAfterRunes(view[rightStart:], r.MaxMatchRunes+1)]

	leftEnd -= offset
	rightStart -= offset

	var matches []Match

	for from := 0; from < leftEnd && len(matches) < max(limit, 1); {
		location := r.Pattern.FindStringIndex(text[from:])
		if len(location) == 0 || from+location[0] >= leftEnd {
			break
		}

		start, end := from+location[0], from+location[1]
		if end > rightStart {
			matches = append(matches, newRuleMatch(r, segment, text[start:end], weight))
			from = end

			continue
		}

		_, size := utf8.DecodeRuneInString(text[start:])

		from = start + size
	}

	return matches
}

func (segment textSegment) aggregateViewSplit(view, text string) (int, int) {
	runes := 0

	for _, chunk := range segment.rawChunks {
		switch view {
		case viewAggregateNorm:
			runes += chunk.NormRunes
		case viewAggregateJoined:
			runes += chunk.JoinedRunes
		default:
			runes += chunk.RawRunes
		}
	}

	leftEnd := byteOffsetAfterRunes(text, runes)

	if view == viewAggregateNorm {
		return leftEnd, min(len(text), leftEnd+len(guardBoundaryMarker))
	}

	return leftEnd, leftEnd
}

func byteOffsetBeforeRunes(text string, end, count int) int {
	for ; count > 0 && end > 0; count-- {
		_, size := utf8.DecodeLastRuneInString(text[:end])

		end -= size
	}

	return end
}

func byteOffsetAfterRunes(text string, count int) int {
	offset := 0

	for ; count > 0 && offset < len(text); count-- {
		_, size := utf8.DecodeRuneInString(text[offset:])

		offset += size
	}

	return offset
}

func (r *compiledRule) matchSegmentText(segment textSegment, policy compiledPolicy, limit int) []Match {
	text, weight, ok := r.matchInput(segment, policy)
	if !ok {
		return nil
	}

	switch r.Type {
	case ruleTypeRegex:
		return r.matchRegexSegment(segment, text, weight, limit)
	case ruleTypePhrase:
		return r.matchPhraseSegment(segment, text, weight, limit)
	default:
		return nil
	}
}

func (r *compiledRule) matchInput(segment textSegment, policy compiledPolicy) (string, float64, bool) {
	if !r.appliesToTextSegment(segment) {
		return "", 0, false
	}

	if segment.Aggregate && r.View != viewRaw && r.View != viewAggregateNorm && r.View != viewAggregateJoined {
		return "", 0, false
	}

	text := segmentView(segment, r.View)
	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}

	if !r.literalPrefilterPasses(segment, text) {
		return "", 0, false
	}

	weight := r.Weight * segmentWeightMultiplier(policy, segment) * policy.viewMultiplier(r.View)

	return text, weight, weight > 0
}

func (r *compiledRule) literalPrefilterPasses(segment textSegment, text string) bool {
	if r.View != viewRaw {
		slot := literalSlotNorm

		if r.View == viewJoined || r.View == viewAggregateJoined {
			slot = literalSlotJoined
		}

		if passed, ok := r.cachedLiteralsPass(segment, slot, text); ok {
			return passed
		}

		if len(r.RequiredLiteralBranches) > 0 {
			return containsAnyLiteralBranch(text, r.RequiredLiteralBranches)
		}

		return containsAllLiteralGroups(text, r.RequiredLiteralGroups)
	}

	slot := literalSlotNorm

	if segment.Aggregate {
		slot = literalSlotAggregateRawNorm
	}

	passed, ok := r.cachedLiteralsPass(segment, slot, segment.rawNormalizedView())
	if !ok {
		passed = containsAllLiteralGroups(segment.rawNormalizedView(), r.RequiredLiteralGroups)
	}

	if passed {
		return true
	}

	return len(r.RawCasePrefilter) > 0 &&
		containsAllLiteralGroups(segment.rawNormalizedView(), r.RawCaseStablePrefilter) &&
		containsASCIIUpper(text) &&
		containsAllLiteralGroupsASCIIFold(text, r.RawCasePrefilter)
}

func containsAnyLiteralBranch(text string, branches [][][]string) bool {
	for _, branch := range branches {
		if containsAllLiteralGroups(text, branch) {
			return true
		}
	}

	return false
}

func containsASCIIUpper(text string) bool {
	for index := range len(text) {
		if text[index] >= 'A' && text[index] <= 'Z' {
			return true
		}
	}

	return false
}

func containsAllLiteralGroupsASCIIFold(text string, groups [][]string) bool {
	for _, group := range groups {
		matched := false

		for _, literal := range group {
			if containsASCIIFold(text, literal) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	return true
}

func containsASCIIFold(text, literal string) bool {
	if strings.Contains(text, literal) {
		return true
	}

	if literal == "" || len(literal) > len(text) {
		return literal == ""
	}

	for index := range len(literal) {
		if literal[index] >= 0x80 {
			return false
		}
	}

	for start := 0; start <= len(text)-len(literal); start++ {
		matched := true

		for offset := range len(literal) {
			if foldASCII(text[start+offset]) != foldASCII(literal[offset]) {
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

func foldASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}

	return value
}

func (r *compiledRule) matchRegexSegment(segment textSegment, text string, weight float64, limit int) []Match {
	matches := r.matchRegexText(segment, text, weight, limit)
	if len(matches) > 0 || r.View != viewRaw {
		return matches
	}

	normalized := segment.normalizedRawFallback(text)
	if normalized == text {
		return nil
	}

	return r.matchRegexText(segment, normalized, weight, limit)
}

func (r *compiledRule) matchRegexText(segment textSegment, text string, weight float64, limit int) []Match {
	if limit <= 0 {
		limit = 1
	}

	if limit == 1 {
		location := r.Pattern.FindStringIndex(text)
		if location == nil {
			return nil
		}

		return []Match{newRuleMatch(r, segment, text[location[0]:location[1]], weight)}
	}

	spans := r.Pattern.FindAllString(text, limit)
	matches := make([]Match, 0, len(spans))

	for _, span := range spans {
		matches = append(matches, newRuleMatch(r, segment, span, weight))
	}

	return matches
}

func (r *compiledRule) matchPhraseSegment(segment textSegment, text string, weight float64, limit int) []Match {
	matches := r.matchPhraseText(segment, text, weight, limit)
	if len(matches) > 0 || r.View != viewRaw {
		return matches
	}

	normalized := segment.normalizedRawFallback(text)
	if normalized == text {
		return nil
	}

	return r.matchPhraseText(segment, normalized, weight, limit)
}

func (segment textSegment) rawNormalizedView() string {
	if segment.Aggregate {
		return segment.rawNorm
	}

	return segment.Views.Norm
}

func (segment textSegment) normalizedRawFallback(text string) string {
	switch {
	case segment.Aggregate:
		return segment.rawNorm
	case segment.piece != nil && text == segment.piece.views.Raw:
		return segment.piece.normalizedRaw()
	default:
		return guardtext.NormalizeView(text)
	}
}

func (r *compiledRule) matchPhraseText(segment textSegment, text string, weight float64, limit int) []Match {
	if limit <= 0 {
		limit = 1
	}

	matches := make([]Match, 0, min(limit, len(r.Phrases)))
	for _, phrase := range r.Phrases {
		if !phraseMatches(text, phrase, r.MatchMode) {
			continue
		}

		matches = append(matches, newRuleMatch(r, segment, phrase, weight))
		if len(matches) >= limit {
			break
		}
	}

	return matches
}

func containsAnyLiteral(text string, literals []string) bool {
	for _, literal := range literals {
		if strings.Contains(text, literal) {
			return true
		}
	}

	return false
}

func containsAllLiteralGroups(text string, groups [][]string) bool {
	for _, group := range groups {
		if !containsAnyLiteral(text, group) {
			return false
		}
	}

	return true
}

func segmentView(segment textSegment, view string) string {
	switch view {
	case viewRaw:
		return segment.Views.Raw
	case viewJoined, viewAggregateJoined:
		return segment.Views.Joined
	default:
		return segment.Views.Norm
	}
}

func newRuleMatch(rule *compiledRule, segment textSegment, span string, weight float64) Match {
	return Match{
		ID:      rule.ID,
		Family:  rule.Family,
		Action:  rule.Action,
		View:    rule.View,
		Segment: string(segment.Kind),
		span:    trimSpan(span),
		Weight:  weight,
	}
}

func segmentWeightMultiplier(policy compiledPolicy, segment textSegment) float64 {
	if !segment.Aggregate || segment.Kinds == 0 {
		return policy.segmentMultiplier(segment.Kind)
	}

	multiplier := 0.0

	for _, kind := range allSegmentKinds {
		if segment.Kinds.contains(kind) {
			multiplier = max(multiplier, policy.segmentMultiplier(kind))
		}
	}

	return multiplier
}

func phraseMatches(text, phrase, mode string) bool {
	if mode != phraseMatchToken {
		return strings.Contains(text, phrase)
	}

	for start := 0; start <= len(text)-len(phrase); {
		index := strings.Index(text[start:], phrase)
		if index < 0 {
			return false
		}

		index += start

		beforeOK := index == 0 || !isTokenRuneBefore(text, index)
		after := index + len(phrase)
		afterOK := after == len(text) || !isTokenRuneAfter(text, after)

		if beforeOK && afterOK {
			return true
		}

		start = index + 1
	}

	return false
}

func isTokenRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])

	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isTokenRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])

	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func trimSpan(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 120 {
		return value
	}

	return value[:120]
}

func distinctPositiveFamilies(hits []Match) []string {
	set := make(map[string]struct{})

	for _, hit := range hits {
		if hit.Family == "" {
			continue
		}

		set[hit.Family] = struct{}{}
	}

	families := make([]string, 0, len(set))
	for family := range set {
		families = append(families, family)
	}

	slices.Sort(families)

	return families
}
