package promptguard

import (
	"slices"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

type decodeRegionHits struct {
	start, end int
	hits       []Match
}

type decodeRuleEvidence struct {
	rule       *compiledRule
	base       []Match
	best       []Match
	regions    []decodeRegionHits
	graph      []Match
	original   []Match
	confirmed  *decodeSelectionPath
	seen       map[*decodeSelectionPath]struct{}
	checkedRaw bool
}

// 독립 해석은 그래프로 유지하고 실제 문자열의 정식 규칙 매치만 점수에 반영한다.
type decodedRegionEvaluation struct {
	guard   *Guard
	policy  compiledPolicy
	rules   []decodeRuleEvidence
	decoder guardtext.RuleRegionDecoder
	status  guardtext.DecodeStatus
}

func (g *Guard) evaluateDecodedRegions(input string, base Evaluation) (Evaluation, bool) {
	if !guardtext.HasIndependentRuleRegions(input) {
		return Evaluation{}, false
	}

	policy := g.policy()
	prepared, semantic := guardtext.PrepareRuleInput(input, g.decodedCandidateMayContribute)

	if len(semantic.Candidates) > 0 {
		scored, overflow := partitionDecodedSegments(semantic)

		scored = append(scored, g.blockingOverflowSegments(base, scored, overflow)...)
		base = g.evaluateSegmentsWithBase(policy, base, scored)
	}

	scan := decodedRegionEvaluation{guard: g, policy: policy, rules: g.decodeEvidence(base), status: semantic.Status}

	for _, root := range guardtext.RuleDecodeInputs(prepared) {
		scan.evaluateRoot(root)
	}

	return scan.evaluation(), true
}

func (scan *decodedRegionEvaluation) evaluateRoot(root string) {
	for index := range scan.rules {
		scan.rules[index].resetRoot()
	}

	regions := scan.decodeRoot(root)
	parts := decodeSourceParts(root, regions)
	proof := decodeLiteralProof{filter: &scan.guard.decodeProof}

	proof.states[0] = 1

	// 해석 영역이 없으면 그래프에 해석 선택 간선이 없어 observeGraph가 모든 일치를 버리므로 그래프를 훑지 않는다.
	matchGraph := len(regions) > 0
	if matchGraph && len(scan.guard.decodeProof.automaton.nodes) > 0 {
		for _, part := range parts {
			proof.visit(part.values)
		}
	}

	graphs := make(map[string]decodeViewGraph)

	for index := range scan.rules {
		entry := &scan.rules[index]
		if matchGraph && !entry.saturated() && proof.ruleMayContribute(entry.rule) {
			scan.matchRule(root, parts, entry, graphs)
		}

		entry.commitRoot()
	}
}

func (scan *decodedRegionEvaluation) decodeRoot(root string) []guardtext.RuleDecodeRegion {
	var regions []guardtext.RuleDecodeRegion

	status := scan.decoder.Walk(root, func(start, end int, value string) {
		scan.observeSegment(root, start, end, decodedCandidateSegment(value))
	}, func(region guardtext.RuleDecodeRegion) {
		region = scan.filterRegion(root, region)
		if len(region.Readings) > 0 {
			regions = append(regions, region)
		}
	})

	scan.status |= status

	return regions
}

func (scan *decodedRegionEvaluation) filterRegion(root string, region guardtext.RuleDecodeRegion) guardtext.RuleDecodeRegion {
	kept := region.Readings[:0]
	for _, reading := range region.Readings {
		context, bounded := guardtext.RuleRegionContext(root, region, reading)
		if !bounded {
			scan.status |= guardtext.DecodeByteLimit
			continue
		}

		if context != "" {
			scan.observeCoupledContext(root, region, context)

			continue
		}

		kept = append(kept, reading)
	}

	region.Readings = kept

	return region
}

func (scan *decodedRegionEvaluation) observeCoupledContext(root string, region guardtext.RuleDecodeRegion, context string) {
	segments, status := scan.guard.decodedTextSegmentsWithRaw(context, Evaluation{}, &scan.decoder)

	scan.status |= status

	for _, segment := range segments {
		scan.observeSegment(root, region.Start, region.End, segment)
	}

	scan.observeSegment(root, region.Start, region.End, decodedCandidateSegment(context))
}

func (scan *decodedRegionEvaluation) observeSegment(root string, start, end int, segment textSegment) {
	for index := range scan.rules {
		scan.rules[index].observeRegion(root, start, end, segment, scan.policy)
	}
}

func (scan *decodedRegionEvaluation) matchRule(root string, parts []decodeSourcePart, entry *decodeRuleEvidence, graphs map[string]decodeViewGraph) {
	views := []string{entry.rule.View}
	if entry.rule.View == viewRaw {
		views = append(views, viewNorm)
	}

	for _, view := range views {
		graph, exists := graphs[view]
		if !exists {
			graph = buildDecodeViewGraph(parts, view)
			graphs[view] = graph
		}

		for programIndex := range entry.rule.DecodePrograms {
			matchDecodeGraph(graph, &entry.rule.DecodePrograms[programIndex], func(thread decodeMatchThread) bool {
				return scan.observeGraph(root, entry, thread)
			})

			if entry.saturated() {
				return
			}
		}
	}
}

func (scan *decodedRegionEvaluation) evaluation() Evaluation {
	state := evaluationState{}

	for index := range scan.rules {
		entry := &scan.rules[index]
		state.applyHits(entry.rule, entry.base)
		state.applyHits(entry.rule, entry.best)
	}

	evaluation := state.evaluation(scan.policy)
	if scan.status != 0 {
		evaluation.DecodeIncomplete = true
		evaluation.DecodeLimits = decodeLimitLabels(scan.status)

		if evaluation.Decision == DecisionAllow {
			evaluation.Decision = DecisionReview
		}
	}

	return evaluation
}

func (g *Guard) decodeEvidence(base Evaluation) []decodeRuleEvidence {
	var rules []decodeRuleEvidence

	position := 0

	for packIndex := range g.packs {
		for ruleIndex := range g.packs[packIndex].Rules {
			rule := &g.packs[packIndex].Rules[ruleIndex]
			start := position

			for position < len(base.Hits) && base.Hits[position].ID == rule.ID {
				position++
			}

			rules = append(rules, decodeRuleEvidence{rule: rule, base: base.Hits[start:position]})
		}
	}

	return rules
}

func (entry *decodeRuleEvidence) resetRoot() {
	entry.regions = nil
	entry.graph = nil
	entry.original = nil
	entry.confirmed = nil
	entry.seen = nil
	entry.checkedRaw = false
}

func (entry *decodeRuleEvidence) observeRegion(input string, start, end int, segment textSegment, policy compiledPolicy) {
	remaining := entry.rule.MaxOccurrences - len(entry.base)
	if remaining <= 0 {
		return
	}

	matches := entry.rule.matchSegment(segment, policy, remaining)
	if len(matches) == 0 {
		return
	}

	original := entry.rule.matchSegment(decodedCandidateSegment(input[start:end]), policy, remaining)

	matches = matches[min(len(original), len(matches)):]

	if len(matches) == 0 {
		return
	}

	for index := range entry.regions {
		region := &entry.regions[index]
		if region.start == start && region.end == end {
			if len(matches) > len(region.hits) {
				region.hits = matches
			}

			return
		}
	}

	entry.regions = append(entry.regions, decodeRegionHits{start: start, end: end, hits: matches})
}

func (scan *decodedRegionEvaluation) observeGraph(input string, entry *decodeRuleEvidence, thread decodeMatchThread) bool {
	if entry.saturated() {
		return true
	}

	if thread.path == nil {
		return false
	}

	if _, exists := entry.seen[thread.path]; exists {
		return false
	}

	if entry.seen == nil {
		entry.seen = make(map[*decodeSelectionPath]struct{})
	}

	entry.seen[thread.path] = struct{}{}

	status := scan.decoder.ReserveContext(2 * len(input))

	scan.status |= status

	if status != 0 {
		return true
	}

	witness, combined := materializeDecodePath(input, thread.path, entry.confirmed, thread.start)
	matches := entry.rule.matchSegment(decodedCandidateSegment(witness), scan.policy, entry.rule.MaxOccurrences)

	if !entry.checkedRaw {
		entry.original = entry.rule.matchSegment(decodedCandidateSegment(input), scan.policy, entry.rule.MaxOccurrences)
		entry.checkedRaw = true
	}

	if slices.EqualFunc(matches, entry.original, func(left, right Match) bool { return left.span == right.span }) {
		return false
	}

	if len(matches) > len(entry.graph) {
		entry.graph = matches
		entry.confirmed = combined
	}

	return entry.saturated()
}

func (entry *decodeRuleEvidence) saturated() bool {
	count := len(entry.base)
	for _, region := range entry.regions {
		count += len(region.hits)
	}

	return max(count, len(entry.graph), len(entry.base)+len(entry.best)) >= entry.rule.MaxOccurrences
}

func (entry *decodeRuleEvidence) commitRoot() {
	remaining := entry.rule.MaxOccurrences - len(entry.base)

	var matches []Match

	for _, region := range entry.regions {
		count := min(remaining-len(matches), len(region.hits))

		matches = append(matches, region.hits[:count]...)
	}

	if len(entry.graph) > len(entry.base)+len(matches) {
		matches = entry.graph[len(entry.base):]
	}

	if len(matches) > len(entry.best) {
		entry.best = matches
	}
}

func (proof *decodeLiteralProof) ruleMayContribute(rule *compiledRule) bool {
	if len(proof.filter.automaton.nodes) == 0 {
		return true
	}

	for index, owner := range proof.filter.owners {
		if owner != rule {
			continue
		}

		complete := true

		for _, group := range proof.filter.rules[index] {
			if !proof.groupMatched(group) {
				complete = false
				break
			}
		}

		if complete {
			return true
		}
	}

	return false
}
