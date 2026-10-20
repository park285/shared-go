package promptguard

// segmentRuleHits는 입력 길이가 아니라 각 규칙의 occurrence 한도만큼 근거를 보존한다.
// 원문이 경계 aggregate보다 우선하는 기존 평가 순서를 유지한다.
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

	var storage [32]segmentRuleHits

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

// evaluateSegmentsWithBase는 원문을 재검사하지 않고 남은 규칙별 한도에만 decode 근거를 더한다.
// 기존 평가인 base는 같은 Guard가 규칙 순서로 생성한 결과여야 한다.
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
