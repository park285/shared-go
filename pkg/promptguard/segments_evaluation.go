package promptguard

type segmentRuleHits struct {
	rule      *compiledRule
	raw       []Match
	aggregate []Match
}

func (g *Guard) evaluateTextSegments(input string) Evaluation {
	policy := g.policy()
	ruleCount := 0

	for _, pack := range g.packs {
		ruleCount += len(pack.Rules)
	}

	var storage [64]segmentRuleHits

	rules := storage[:0]

	if ruleCount > len(storage) {
		rules = make([]segmentRuleHits, 0, ruleCount)
	}

	for packIndex := range g.packs {
		for ruleIndex := range g.packs[packIndex].Rules {
			rules = append(rules, segmentRuleHits{rule: &g.packs[packIndex].Rules[ruleIndex]})
		}
	}

	walkEvaluationSegments(input, g.aggregateMayMatch, func(segment textSegment) {
		for index := range rules {
			entry := &rules[index]
			if remaining := entry.rule.MaxOccurrences - len(entry.raw); remaining > 0 {
				matched := entry.rule.matchSegment(segment, policy, remaining)
				if len(entry.raw) == 0 {
					entry.raw = matched
				} else {
					entry.raw = append(entry.raw, matched...)
				}
			}
		}
	}, func(segment textSegment) {
		for index := range rules {
			entry := &rules[index]
			if len(entry.raw) == entry.rule.MaxOccurrences {
				continue
			}

			if remaining := entry.rule.MaxOccurrences - len(entry.aggregate); remaining > 0 {
				matched := entry.rule.matchSegment(segment, policy, remaining)
				if len(entry.aggregate) == 0 {
					entry.aggregate = matched
				} else {
					entry.aggregate = append(entry.aggregate, matched...)
				}
			}
		}
	})

	state := evaluationState{hits: make([]Match, 0)}

	for _, entry := range rules {
		state.applyHits(entry.rule, entry.raw)

		remaining := min(entry.rule.MaxOccurrences-len(entry.raw), len(entry.aggregate))
		state.applyHits(entry.rule, entry.aggregate[:remaining])
	}

	return state.evaluation(policy)
}

func (g *Guard) evaluateSegmentsWithBase(policy compiledPolicy, base Evaluation, segments []textSegment) Evaluation {
	state := evaluationState{hits: make([]Match, 0, len(base.Hits))}
	baseIndex := 0

	for _, pack := range g.packs {
		for ruleIndex := range pack.Rules {
			rule := &pack.Rules[ruleIndex]
			start := baseIndex

			for baseIndex < len(base.Hits) && base.Hits[baseIndex].ID == rule.ID {
				baseIndex++
			}

			state.applyHits(rule, base.Hits[start:baseIndex])

			remaining := rule.MaxOccurrences - (baseIndex - start)
			if remaining > 0 {
				state.collectRuleHits(rule, segments, policy, remaining)
			}
		}
	}

	return state.evaluation(policy)
}
