package promptguard

import (
	"slices"
	"strings"
)

func (r *compiledRule) appliesToSegment(kind segmentKind) bool {
	_, ok := r.Segments[kind]
	return ok
}

func (r *compiledRule) matchSegment(segment textSegment, policy compiledPolicy) []Match {
	if !r.appliesToSegment(segment.Kind) {
		return nil
	}

	text := segmentView(segment, r.View)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	weight := r.Weight * policy.segmentMultiplier(segment.Kind) * policy.viewMultiplier(r.View)
	if weight <= 0 {
		return nil
	}

	matches := make([]Match, 0, 1)

	switch r.Type {
	case ruleTypeRegex:
		span := r.Pattern.FindString(text)
		if span == "" {
			return nil
		}

		matches = append(matches, Match{
			ID:      r.ID,
			Family:  r.Family,
			Action:  r.Action,
			View:    r.View,
			Segment: string(segment.Kind),
			span:    trimSpan(span),
			Weight:  weight,
		})
	case ruleTypePhrase:
		for _, phrase := range r.Phrases {
			if !strings.Contains(text, phrase) {
				continue
			}

			matches = append(matches, Match{
				ID:      r.ID,
				Family:  r.Family,
				Action:  r.Action,
				View:    r.View,
				Segment: string(segment.Kind),
				span:    trimSpan(phrase),
				Weight:  weight,
			})

			break
		}
	}

	return matches
}

func segmentView(segment textSegment, view string) string {
	switch view {
	case viewRaw:
		return segment.Views.Raw
	case viewJoined:
		return segment.Views.Joined
	default:
		return segment.Views.Norm
	}
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
		if hit.Action == hitActionDampen || hit.Family == "" {
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
