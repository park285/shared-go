package promptguard

import (
	"slices"
)

const requiredLiteralCapacity = 512

type requiredLiteralSet [requiredLiteralCapacity / 64]uint64

func (set *requiredLiteralSet) add(number int) {
	set[number/64] |= 1 << (number % 64)
}

func (set *requiredLiteralSet) union(other *requiredLiteralSet) {
	for index := range set {
		set[index] |= other[index]
	}
}

func (set *requiredLiteralSet) intersects(mask *requiredLiteralSet) bool {
	for index := range set {
		if set[index]&mask[index] != 0 {
			return true
		}
	}

	return false
}

type requiredLiteralFilter struct {
	automaton aggregateLiteralAutomaton
	outputs   []requiredLiteralSet
}

func (filter *requiredLiteralFilter) scan(text string) requiredLiteralSet {
	var found requiredLiteralSet

	state := aggregateAutomatonState(0)

	for index := range len(text) {
		transition := filter.automaton.nodes[state].next[text[index]]

		state = transition & aggregateAutomatonStateMask
		if transition&aggregateAutomatonOutput != 0 {
			found.union(&filter.outputs[state])
		}
	}

	return found
}

const (
	literalSlotNorm = iota
	literalSlotJoined
	literalSlotAggregateRawNorm
	literalSlotCount
)

type segmentLiterals struct {
	sets  [literalSlotCount]requiredLiteralSet
	ready [literalSlotCount]bool
}

func (cache *segmentLiterals) find(filter *requiredLiteralFilter, slot int, text string) *requiredLiteralSet {
	if !cache.ready[slot] {
		cache.sets[slot], cache.ready[slot] = filter.scan(text), true
	}

	return &cache.sets[slot]
}

func requiredLiteralBranchesPass(found *requiredLiteralSet, branches [][]requiredLiteralSet) bool {
	for _, branch := range branches {
		complete := true

		for index := range branch {
			if !found.intersects(&branch[index]) {
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

func attachRequiredLiteralFilter(packs []compiledPack) {
	literals, usable := requiredLiteralList(packs)
	if !usable {
		return
	}

	filter, numbers, built := newRequiredLiteralFilter(literals)
	if !built {
		return
	}

	for _, rule := range requiredLiteralRules(packs) {
		branches := requiredLiteralBranches(rule)
		masks := make([][]requiredLiteralSet, len(branches))

		for branchIndex, branch := range branches {
			masks[branchIndex] = make([]requiredLiteralSet, len(branch))

			for groupIndex, group := range branch {
				for _, literal := range group {
					masks[branchIndex][groupIndex].add(numbers[literal])
				}
			}
		}

		rule.requiredLiterals = filter
		rule.requiredLiteralMasks = masks
	}
}

func requiredLiteralList(packs []compiledPack) ([]string, bool) {
	var literals []string

	for _, rule := range requiredLiteralRules(packs) {
		for _, branch := range requiredLiteralBranches(rule) {
			for _, group := range branch {
				if slices.Contains(group, "") {
					return nil, false
				}

				literals = append(literals, group...)
			}
		}
	}

	slices.Sort(literals)

	literals = slices.Compact(literals)

	return literals, len(literals) > 0 && len(literals) <= requiredLiteralCapacity
}

func newRequiredLiteralFilter(literals []string) (*requiredLiteralFilter, map[string]int, bool) {
	automaton, complete := newAggregateLiteralAutomaton(literals)
	if !complete {
		return nil, nil, false
	}

	filter := &requiredLiteralFilter{automaton: automaton, outputs: make([]requiredLiteralSet, len(automaton.nodes))}
	terminals := make([]requiredLiteralSet, len(automaton.nodes))
	numbers := make(map[string]int, len(literals))

	for number, literal := range literals {
		terminals[filter.terminal(literal)].add(number)

		numbers[literal] = number
	}

	for state := range automaton.nodes {
		for current := aggregateAutomatonState(state); ; current = automaton.nodes[current].fail {
			filter.outputs[state].union(&terminals[current])

			if current == 0 {
				break
			}
		}
	}

	return filter, numbers, true
}

func (filter *requiredLiteralFilter) terminal(literal string) aggregateAutomatonState {
	state := aggregateAutomatonState(0)

	for index := range len(literal) {
		state = filter.automaton.nodes[state].next[literal[index]] & aggregateAutomatonStateMask
	}

	return state
}

func requiredLiteralRules(packs []compiledPack) []*compiledRule {
	var rules []*compiledRule

	for packIndex := range packs {
		for ruleIndex := range packs[packIndex].Rules {
			rules = append(rules, &packs[packIndex].Rules[ruleIndex])
		}
	}

	return rules
}

func requiredLiteralBranches(rule *compiledRule) [][][]string {
	if rule.View != viewRaw && len(rule.RequiredLiteralBranches) > 0 {
		return rule.RequiredLiteralBranches
	}

	return [][][]string{rule.RequiredLiteralGroups}
}

func (segment textSegment) literalCache() *segmentLiterals {
	switch {
	case segment.piece == nil:
		return nil
	case segment.Aggregate:
		return segment.piece.aggregateLiterals
	default:
		return &segment.piece.literals
	}
}

func (r *compiledRule) cachedLiteralsPass(segment textSegment, slot int, text string) (passed, ok bool) {
	cache := segment.literalCache()
	if r.requiredLiterals == nil || cache == nil {
		return false, false
	}

	return requiredLiteralBranchesPass(cache.find(r.requiredLiterals, slot, text), r.requiredLiteralMasks), true
}
