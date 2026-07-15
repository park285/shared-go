package promptguard

import (
	"slices"
)

const maxAggregateAutomatonNodes = 2048

type aggregatePrefilterSet struct {
	joined aggregateViewPrefilter
}

type aggregateViewPrefilter struct {
	automaton  aggregateLiteralAutomaton
	hasRules   bool
	unfiltered bool
}

func compileAggregatePrefilters(packs []compiledPack) aggregatePrefilterSet {
	var literals []string
	unfiltered := false
	for packIndex := range packs {
		for ruleIndex := range packs[packIndex].Rules {
			rule := &packs[packIndex].Rules[ruleIndex]
			if rule.View != viewRaw && rule.View != viewAggregateNorm && rule.View != viewAggregateJoined {
				continue
			}
			if len(rule.AggregatePrefilter) == 0 {
				unfiltered = true
				continue
			}
			for _, literal := range rule.AggregatePrefilter {
				joined := normalizeViews(literal).Joined
				if joined == "" {
					unfiltered = true
					continue
				}
				literals = append(literals, joined)
			}
		}
	}
	return aggregatePrefilterSet{
		joined: newAggregateViewPrefilter(literals, unfiltered),
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
	next   [256]uint32
	fail   uint32
	output bool
}

func newAggregateLiteralAutomaton(literals []string) (aggregateLiteralAutomaton, bool) {
	automaton := aggregateLiteralAutomaton{nodes: make([]aggregateAutomatonNode, 1)}
	for _, literal := range literals {
		state := uint32(0)
		for index := range len(literal) {
			value := literal[index]
			next := automaton.nodes[state].next[value]
			if next == 0 {
				if len(automaton.nodes) >= maxAggregateAutomatonNodes {
					return aggregateLiteralAutomaton{}, false
				}
				next = uint32(len(automaton.nodes))
				automaton.nodes = append(automaton.nodes, aggregateAutomatonNode{})
				automaton.nodes[state].next[value] = next
			}
			state = next
		}
		automaton.nodes[state].output = true
	}
	automaton.buildFailureTransitions()
	return automaton, true
}

func (automaton *aggregateLiteralAutomaton) buildFailureTransitions() {
	queue := make([]uint32, 0, len(automaton.nodes))
	for value := range 256 {
		child := automaton.nodes[0].next[byte(value)]
		if child != 0 {
			queue = append(queue, child)
		}
	}
	for head := 0; head < len(queue); head++ {
		state := queue[head]
		failure := automaton.nodes[state].fail
		for value := range 256 {
			byteValue := byte(value)
			child := automaton.nodes[state].next[byteValue]
			if child == 0 {
				automaton.nodes[state].next[byteValue] = automaton.nodes[failure].next[byteValue]
				continue
			}
			automaton.nodes[child].fail = automaton.nodes[failure].next[byteValue]
			if automaton.nodes[automaton.nodes[child].fail].output {
				automaton.nodes[child].output = true
			}
			queue = append(queue, child)
		}
	}
}

func (automaton aggregateLiteralAutomaton) matches(text []byte) bool {
	state := uint32(0)
	for _, value := range text {
		state = automaton.nodes[state].next[value]
		if automaton.nodes[state].output {
			return true
		}
	}
	return false
}

func (filters aggregatePrefilterSet) mayMatch(tail *aggregateTail, right textSegment) bool {
	if !filters.joined.hasRules {
		return false
	}
	if filters.joined.unfiltered {
		return true
	}
	var buffer aggregateViewBuffer
	text := buffer.buildJoined(tail, right)
	return filters.joined.automaton.matches(text)
}

type aggregateViewBuffer struct {
	data [rollingViewCapacity]byte
}

func (buffer *aggregateViewBuffer) buildJoined(tail *aggregateTail, right textSegment) []byte {
	length := appendAggregateBytes(buffer.data[:], tail.joined.data, "", firstRunes(right.Views.Joined, boundaryWindowRunes))
	return buffer.data[:length]
}

func appendAggregateBytes(destination []byte, left []byte, separator, right string) int {
	position := copy(destination, left)
	position += copy(destination[position:], separator)
	position += copy(destination[position:], right)
	return position
}
