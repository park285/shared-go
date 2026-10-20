package promptguard

import (
	"slices"
)

// requiredLiteralCapacity는 조각 하나의 리터럴 존재 집합이 담을 수 있는 필수 리터럴 수다.
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

// requiredLiteralFilter는 모든 규칙의 필수 리터럴을 한 번에 찾는 오토마톤이다. 규칙마다 같은 조각
// view를 strings.Contains로 다시 훑는 대신, view를 한 번 읽어 어떤 리터럴이 들어 있는지 기록한다.
type requiredLiteralFilter struct {
	automaton aggregateLiteralAutomaton
	// outputs는 상태에서 끝나는 리터럴과 실패 링크로 이어지는 접미 리터럴의 번호 집합이다.
	outputs []requiredLiteralSet
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

// 세그먼트에서 필수 리터럴을 찾는 view 문자열 종류다. 원문 view 규칙이 보는 정규화 원문은 조각 세그먼트에서
// Views.Norm이고 결합 세그먼트에서는 경계 창을 따로 정규화한 rawNorm이다.
const (
	literalSlotNorm = iota
	literalSlotJoined
	literalSlotAggregateRawNorm
	literalSlotCount
)

// segmentLiterals는 세그먼트 view마다 필수 리터럴 오토마톤을 한 번만 돌린 결과다. 세그먼트와 함께 만들고 방문 중에만 쓴다.
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

// requiredLiteralBranchesPass는 containsAnyLiteralBranch와 같은 판정을 리터럴 집합으로 한다. 묶음이 없는
// 대안은 통과한다.
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

// attachRequiredLiteralFilter는 모든 규칙이 함께 쓸 필수 리터럴 오토마톤을 만들어 각 규칙에 붙인다.
// 리터럴이 상한을 넘거나 빈 리터럴이 있으면 붙이지 않고, 규칙은 문자열 검색으로 같은 판정을 한다.
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

// requiredLiteralList는 모든 규칙의 필수 리터럴을 정렬해 중복 없이 돌려준다. 빈 리터럴이 있거나 상한을 넘으면 쓸 수 없다.
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

// newRequiredLiteralFilter는 literals의 오토마톤과 리터럴별 번호를 만든다. 번호는 literals의 순서다.
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

// terminal은 출력 비트를 뺀 전이로 literal을 끝까지 읽은 상태다. 실패 전이를 채운 뒤에도 트라이 경로는 그대로다.
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

// requiredLiteralBranches는 literalPrefilterPasses가 쓰는 대안 목록이다. 원문 view 규칙은 정규화 원문에서 묶음만 본다.
func requiredLiteralBranches(rule *compiledRule) [][][]string {
	if rule.View != viewRaw && len(rule.RequiredLiteralBranches) > 0 {
		return rule.RequiredLiteralBranches
	}

	return [][][]string{rule.RequiredLiteralGroups}
}

// literalCache는 세그먼트 view의 필수 리터럴 캐시다. 캐시는 walkEvaluationSegments가 만든 세그먼트에만 있다. 결합
// 세그먼트의 캐시는 오른쪽 조각이 가리키며, 조각은 바로 앞 경계의 결합 세그먼트 하나에서만 오른쪽이 된다.
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

// cachedLiteralsPass는 세그먼트 캐시로 필수 리터럴 판정을 한다. 오토마톤이나 캐시가 없으면 ok가 false다.
func (r *compiledRule) cachedLiteralsPass(segment textSegment, slot int, text string) (passed, ok bool) {
	cache := segment.literalCache()
	if r.requiredLiterals == nil || cache == nil {
		return false, false
	}

	return requiredLiteralBranchesPass(cache.find(r.requiredLiterals, slot, text), r.requiredLiteralMasks), true
}
