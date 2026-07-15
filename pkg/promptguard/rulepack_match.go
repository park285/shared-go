package promptguard

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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

func (r *compiledRule) matchSegment(segment textSegment, policy compiledPolicy, limit int) []Match {
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
	prefilterText := text
	if r.View == viewRaw {
		prefilterText = segment.rawNormalizedView()
	}
	if !containsAllLiteralGroups(prefilterText, r.RequiredLiteralGroups) {
		return "", 0, false
	}

	weight := r.Weight * segmentWeightMultiplier(policy, segment) * policy.viewMultiplier(r.View)

	return text, weight, weight > 0
}

func (r *compiledRule) matchRegexSegment(segment textSegment, text string, weight float64, limit int) []Match {
	matches := r.matchRegexText(segment, text, weight, limit)
	if len(matches) > 0 || r.View != viewRaw {
		return matches
	}

	normalized := normalizeViews(text).Norm
	if segment.Aggregate {
		normalized = segment.rawNormalizedView()
	}
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

	normalized := normalizeViews(text).Norm
	if segment.Aggregate {
		normalized = segment.rawNormalizedView()
	}
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
