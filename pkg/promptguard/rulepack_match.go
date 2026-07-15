package promptguard

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (r *compiledRule) appliesToSegment(kind segmentKind) bool {
	_, ok := r.Segments[kind]
	return ok
}

func (r *compiledRule) appliesToTextSegment(segment textSegment) bool {
	if !segment.Aggregate {
		return r.appliesToSegment(segment.Kind)
	}

	return slices.ContainsFunc(segment.Kinds, r.appliesToSegment)
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
	if segment.Aggregate && r.View != viewAggregateNorm && r.View != viewAggregateJoined {
		return "", 0, false
	}

	text := segmentView(segment, r.View)
	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}
	// Required literals are compiled in the matcher's own representation. A raw
	// rule can intentionally contain compatibility characters, provider syntax,
	// or other bytes that normalize to a different surface. Prefiltering those
	// rules against the normalized view can therefore create false negatives.
	// Keep the optimization for normalized/joined views, but always execute raw
	// matchers against the raw surface.
	if r.View != viewRaw && !containsAllLiteralGroups(text, r.RequiredLiteralGroups) {
		return "", 0, false
	}

	weight := r.Weight * segmentWeightMultiplier(policy, segment) * policy.viewMultiplier(r.View)

	return text, weight, weight > 0
}

func (r *compiledRule) matchRegexSegment(segment textSegment, text string, weight float64, limit int) []Match {
	if limit <= 0 {
		limit = 1
	}
	spans := r.Pattern.FindAllString(text, limit)
	matches := make([]Match, 0, len(spans))
	for _, span := range spans {
		matches = append(matches, newRuleMatch(r, segment, span, weight))
	}

	return matches
}

func (r *compiledRule) matchPhraseSegment(segment textSegment, text string, weight float64, limit int) []Match {
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
	if !segment.Aggregate || len(segment.Kinds) == 0 {
		return policy.segmentMultiplier(segment.Kind)
	}
	multiplier := 0.0
	for _, kind := range segment.Kinds {
		multiplier = max(multiplier, policy.segmentMultiplier(kind))
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
