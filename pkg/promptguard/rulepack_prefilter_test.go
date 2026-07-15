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
	for _, pack := range guard.packs {
		for _, rule := range pack.Rules {
			var filter aggregateViewPrefilter
			var candidateView func(string) string
			switch rule.View {
			case viewRaw:
				filter = guard.aggregateFilter.rawNorm
				candidateView = normalizeText
			case viewAggregateNorm:
				filter = guard.aggregateFilter.norm
				candidateView = func(value string) string { return normalizeViews(value).Norm }
			case viewAggregateJoined:
				filter = guard.aggregateFilter.joined
				candidateView = func(value string) string { return normalizeViews(value).Joined }
			default:
				continue
			}
			if !filter.hasRules || filter.unfiltered {
				t.Fatalf("rule %q filter = %#v, want a complete literal filter", rule.ID, filter)
			}
			for _, literal := range rule.AggregatePrefilter {
				candidate := candidateView("ordinary " + literal + " context")
				if !filter.automaton.matchesString(candidate) {
					t.Errorf("rule %q literal %q is absent from the aggregate prefilter", rule.ID, literal)
				}
			}
		}
	}
}

func TestAggregatePrefilterKeepsRawPunctuationSplit(t *testing.T) {
	t.Parallel()

	rule, err := compileRule(&rawRule{
		ID:             "raw-punctuation-split",
		Family:         "raw-punctuation-split",
		Type:           ruleTypePhrase,
		Action:         hitActionBlock,
		View:           viewRaw,
		Segments:       []string{string(segmentPlain)},
		Phrases:        []string{"zxq!!!!!jkv"},
		MatchMode:      phraseMatchSubstring,
		Weight:         1,
		MaxOccurrences: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	filter := compileAggregatePrefilters([]compiledPack{{Rules: []compiledRule{rule}}})
	segments, exceeded := buildEvaluationSegmentsFiltered(
		JoinParts("zxq!!", "!!!jkv"),
		filter.mayMatch,
	)
	if exceeded {
		t.Fatal("segment budget unexpectedly exceeded")
	}
	policy := compilePolicy(&rawRulepack{Version: 3})
	for _, segment := range segments {
		if segment.Aggregate && len(rule.matchSegment(segment, policy, 1)) == 1 {
			return
		}
	}
	t.Fatal("raw punctuation split was removed by aggregate prefilter")
}

func TestRollingBenchmarkFixtureExercisesAggregatePath(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	segments, exceeded := buildEvaluationSegmentsFiltered(rollingAggregateBenchmarkInput(), guard.aggregateMayMatch)
	if exceeded {
		t.Fatal("segment budget unexpectedly exceeded")
	}
	for _, segment := range segments {
		if segment.Aggregate {
			return
		}
	}
	t.Fatal("rolling benchmark fixture did not exercise an aggregate segment")
}
