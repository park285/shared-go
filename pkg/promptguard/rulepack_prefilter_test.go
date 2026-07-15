package promptguard

import "testing"

func TestEmbeddedRulesHaveRequiredLiteralPrefilters(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	for _, pack := range guard.packs {
		for _, rule := range pack.Rules {
			if len(rule.RequiredLiteralGroups) == 0 {
				t.Errorf("rule %q has no required literal prefilter", rule.ID)
			}
		}
	}
}

func TestAggregatePrefilterFailsOpenAtNodeLimit(t *testing.T) {
	t.Parallel()

	literals := make([]string, maxAggregateAutomatonNodes)
	for index := range literals {
		literals[index] = string(rune(0x1000+index)) + "-unique-literal"
	}
	filter := newAggregateViewPrefilter(literals, false)
	if !filter.hasRules || !filter.unfiltered {
		t.Fatalf("aggregate filter = %#v, want bounded fail-open filter", filter)
	}
}

func TestAggregatePrefilterCoversEveryEmbeddedRuleLiteral(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	filter := guard.aggregateFilter.joined
	if !filter.hasRules || filter.unfiltered {
		t.Fatalf("aggregate filter = %#v, want a complete literal filter", filter)
	}
	for _, pack := range guard.packs {
		for _, rule := range pack.Rules {
			if rule.View != viewRaw && rule.View != viewAggregateNorm && rule.View != viewAggregateJoined {
				continue
			}
			for _, literal := range rule.AggregatePrefilter {
				candidate := normalizeViews("ordinary " + literal + " context").Joined
				if !filter.automaton.matches([]byte(candidate)) {
					t.Errorf("rule %q literal %q is absent from the aggregate prefilter", rule.ID, literal)
				}
			}
		}
	}
}
