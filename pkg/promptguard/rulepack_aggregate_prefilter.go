package promptguard

import (
	"math"
	"slices"
)

const maxAggregateAutomatonNodes = 2048

type aggregateAutomatonState uint16

const (
	aggregateAutomatonOutput    aggregateAutomatonState = 1 << 15
	aggregateAutomatonStateMask                         = aggregateAutomatonOutput - 1
	_                           aggregateAutomatonState = aggregateAutomatonStateMask - (maxAggregateAutomatonNodes - 1)
)

type aggregatePrefilterSet struct {
	rawNorm aggregateViewPrefilter
	norm    aggregateViewPrefilter
	joined  aggregateViewPrefilter
}

type aggregateViewPrefilter struct {
	automaton  aggregateLiteralAutomaton
	hasRules   bool
	unfiltered bool
}

func compileAggregatePrefilters(packs []compiledPack) aggregatePrefilterSet {
	var (
		rawNormLiterals, normLiterals, joinedLiterals       []string
		rawNormUnfiltered, normUnfiltered, joinedUnfiltered bool
	)

	for packIndex := range packs {
		rules := packs[packIndex].Rules
		for ruleIndex := range rules {
			rule := &rules[ruleIndex]

			var (
				literals   *[]string
				unfiltered *bool
			)

			switch rule.View {
			case viewRaw:
				literals = &rawNormLiterals
				unfiltered = &rawNormUnfiltered
			case viewAggregateNorm:
				literals = &normLiterals
				unfiltered = &normUnfiltered
			case viewAggregateJoined:
				literals = &joinedLiterals
				unfiltered = &joinedUnfiltered
			default:
				continue
			}

			if len(rule.AggregatePrefilter) == 0 {
				*unfiltered = true
				continue
			}

			for _, literal := range rule.AggregatePrefilter {
				value := literal //nolint:copyloopvar // 이 사본은 아래에서 변형되므로 루프 변수를 그대로 쓸 수 없다.

				if rule.View == viewAggregateJoined {
					value = normalizeViews(literal).Joined
				}

				if value == "" {
					*unfiltered = true
					continue
				}

				*literals = append(*literals, value)
			}
		}
	}

	return aggregatePrefilterSet{
		rawNorm: newAggregateViewPrefilter(rawNormLiterals, rawNormUnfiltered),
		norm:    newAggregateViewPrefilter(normLiterals, normUnfiltered),
		joined:  newAggregateViewPrefilter(joinedLiterals, joinedUnfiltered),
	}
}

func newAggregateViewPrefilter(literals []string, unfiltered bool) aggregateViewPrefilter {
	slices.Sort(literals)

	literals = slices.Compact(literals)
	if len(literals) == 0 {
		return aggregateViewPrefilter{hasRules: unfiltered, unfiltered: unfiltered}
	}

	automaton, complete := newAggregateLiteralAutomaton(literals)
	if !complete {
		return aggregateViewPrefilter{hasRules: true, unfiltered: true}
	}

	return aggregateViewPrefilter{
		automaton:  automaton,
		hasRules:   true,
		unfiltered: unfiltered,
	}
}

type aggregateLiteralAutomaton struct {
	nodes []aggregateAutomatonNode
}

type aggregateAutomatonNode struct {
	next   [256]aggregateAutomatonState
	fail   aggregateAutomatonState
	output bool
}

func newAggregateLiteralAutomaton(literals []string) (aggregateLiteralAutomaton, bool) {
	automaton := aggregateLiteralAutomaton{nodes: make([]aggregateAutomatonNode, 1)}

	for _, literal := range literals {
		state := aggregateAutomatonState(0)

		for index := range len(literal) {
			value := literal[index]
			next := automaton.nodes[state].next[value]

			if next == 0 {
				if len(automaton.nodes) >= maxAggregateAutomatonNodes {
					return aggregateLiteralAutomaton{}, false
				}

				nodeCount := len(automaton.nodes)
				if nodeCount < 0 || nodeCount > math.MaxUint16 {
					return aggregateLiteralAutomaton{}, false
				}

				next = aggregateAutomatonState(nodeCount)

				automaton.nodes = append(automaton.nodes, aggregateAutomatonNode{})
				automaton.nodes[state].next[value] = next
			}

			state = next
		}

		automaton.nodes[state].output = true
	}

	buildAggregateFailureTransitions(automaton.nodes)
	encodeAggregateOutputTransitions(automaton.nodes)

	return automaton, true
}

func buildAggregateFailureTransitions(nodes []aggregateAutomatonNode) {
	queue := make([]aggregateAutomatonState, 0, len(nodes))

	for value := range 256 {
		child := nodes[0].next[byte(value)]
		if child != 0 {
			queue = append(queue, child)
		}
	}

	for head := 0; head < len(queue); head++ {
		state := queue[head]
		failure := nodes[state].fail

		for value := range 256 {
			byteValue := byte(value)
			child := nodes[state].next[byteValue]

			if child == 0 {
				nodes[state].next[byteValue] = nodes[failure].next[byteValue]
				continue
			}

			nodes[child].fail = nodes[failure].next[byteValue]
			if nodes[nodes[child].fail].output {
				nodes[child].output = true
			}

			queue = append(queue, child)
		}
	}
}

func encodeAggregateOutputTransitions(nodes []aggregateAutomatonNode) {
	for nodeIndex := range nodes {
		for value := range 256 {
			target := nodes[nodeIndex].next[byte(value)]
			if nodes[target].output {
				nodes[nodeIndex].next[byte(value)] = target | aggregateAutomatonOutput
			}
		}
	}
}

func (automaton aggregateLiteralAutomaton) matches(text []byte) bool {
	state := aggregateAutomatonState(0)

	for _, value := range text {
		transition := automaton.nodes[state].next[value]
		if transition&aggregateAutomatonOutput != 0 {
			return true
		}

		state = transition & aggregateAutomatonStateMask
	}

	return false
}

func (automaton aggregateLiteralAutomaton) matchesString(text string) bool {
	state := aggregateAutomatonState(0)

	for index := range len(text) {
		transition := automaton.nodes[state].next[text[index]]
		if transition&aggregateAutomatonOutput != 0 {
			return true
		}

		state = transition & aggregateAutomatonStateMask
	}

	return false
}

func (automaton aggregateLiteralAutomaton) matchesNormalizedASCII(text []byte) (bool, bool) {
	state := aggregateAutomatonState(0)
	pendingSpace := false
	wrote := false

	for _, value := range text {
		replacement, ok := normalizeASCIIByteReplacement(value)
		if !ok {
			return false, false
		}

		if replacement == "" {
			continue
		}

		if replacement == " " {
			pendingSpace = wrote
			continue
		}

		if pendingSpace {
			transition := automaton.nodes[state].next[' ']
			if transition&aggregateAutomatonOutput != 0 {
				return true, true
			}

			state = transition & aggregateAutomatonStateMask
			pendingSpace = false
		}

		for index := range len(replacement) {
			transition := automaton.nodes[state].next[replacement[index]]
			if transition&aggregateAutomatonOutput != 0 {
				return true, true
			}

			state = transition & aggregateAutomatonStateMask
		}

		wrote = true
	}

	return false, true
}

func (filters aggregatePrefilterSet) mayMatch(tail *aggregateTail, right textSegment) bool {
	var buffer aggregateViewBuffer

	if filters.rawNorm.hasRules && filters.rawNorm.mayMatchRawNorm(&buffer, tail, right) {
		return true
	}

	if filters.norm.hasRules {
		if filters.norm.unfiltered || filters.norm.automaton.matches(buffer.buildNorm(tail, right)) {
			return true
		}
	}

	if filters.joined.hasRules {
		return filters.joined.unfiltered || filters.joined.automaton.matches(buffer.buildJoined(tail, right))
	}

	return false
}

func (prefilter aggregateViewPrefilter) mayMatchRawNorm(
	buffer *aggregateViewBuffer,
	tail *aggregateTail,
	right textSegment,
) bool {
	if prefilter.unfiltered {
		return true
	}

	raw := buffer.buildRaw(tail, right)

	if matched, complete := prefilter.automaton.matchesNormalizedASCII(raw); complete {
		return matched
	}

	return prefilter.automaton.matchesString(normalizeText(string(raw)))
}

type aggregateViewBuffer struct {
	data [rollingViewCapacity]byte
}

func (buffer *aggregateViewBuffer) buildRaw(tail *aggregateTail, right textSegment) []byte {
	length := appendAggregateBytes(buffer.data[:], tail.raw.data, "", firstRunes(right.Views.Raw, boundaryWindowRunes))
	return buffer.data[:length]
}

func (buffer *aggregateViewBuffer) buildNorm(tail *aggregateTail, right textSegment) []byte {
	length := appendAggregateBytes(
		buffer.data[:],
		tail.norm.data,
		guardBoundaryMarker,
		firstRunes(right.Views.Norm, boundaryWindowRunes),
	)

	return buffer.data[:length]
}

func (buffer *aggregateViewBuffer) buildJoined(tail *aggregateTail, right textSegment) []byte {
	length := appendAggregateBytes(buffer.data[:], tail.joined.data, "", firstRunes(right.Views.Joined, boundaryWindowRunes))
	return buffer.data[:length]
}

func appendAggregateBytes(destination, left []byte, separator, right string) int {
	position := copy(destination, left)

	position += copy(destination[position:], separator)
	position += copy(destination[position:], right)

	return position
}
