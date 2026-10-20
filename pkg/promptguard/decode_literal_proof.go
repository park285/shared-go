package promptguard

import (
	"math/bits"
	"strings"
	"unicode"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

type decodeLiteralFilter struct {
	automaton aggregateLiteralAutomaton
	rules     [][][]aggregateAutomatonState
	owners    []*compiledRule
}

// 필수 리터럴을 찾지 못한 경우에만 디코딩을 생략한다. 구분자를 모두 제거한 표면은
// raw/norm/joined보다 넓은 해석이므로 매치는 공격 근거가 아니라 증명 불가를 뜻한다.
func compileDecodeLiteralProof(packs []compiledPack) decodeLiteralFilter {
	var (
		literals []string
		rules    [][][]string
		owners   []*compiledRule
	)

	for packIndex := range packs {
		for ruleIndex := range packs[packIndex].Rules {
			rule := &packs[packIndex].Rules[ruleIndex]

			branches := rule.RequiredLiteralBranches
			if len(branches) == 0 {
				branches = [][][]string{rule.RequiredLiteralGroups}
			}

			for _, branch := range branches {
				var groups [][]string

				for _, group := range branch {
					projected, usable := decodeProofLiteralGroup(group)
					if usable {
						groups = append(groups, projected)
						literals = append(literals, projected...)
					}
				}

				if len(groups) == 0 {
					return decodeLiteralFilter{}
				}

				rules = append(rules, groups)
				owners = append(owners, rule)
			}
		}
	}

	filter := newAggregateViewPrefilter(literals, false)
	if filter.unfiltered || !filter.hasRules {
		return decodeLiteralFilter{}
	}

	proof := decodeLiteralFilter{automaton: filter.automaton, owners: owners}

	for _, groups := range rules {
		proof.addBranch(groups)
	}

	return proof
}

func (proof *decodeLiteralFilter) addBranch(groups [][]string) {
	rule := make([][]aggregateAutomatonState, 0, len(groups))

	for _, group := range groups {
		terminals := make([]aggregateAutomatonState, 0, len(group))

		for _, literal := range group {
			state := aggregateAutomatonState(0)

			for index := range len(literal) {
				state = proof.automaton.nodes[state].next[literal[index]] & aggregateAutomatonStateMask
			}

			terminals = append(terminals, state)
		}

		rule = append(rule, terminals)
	}

	proof.rules = append(proof.rules, rule)
}

func decodeProofLiteralGroup(group []string) ([]string, bool) {
	projected := make([]string, 0, len(group))
	for _, literal := range group {
		word := decodeProofWordText(literal)
		if word == "" {
			return nil, false
		}

		projected = append(projected, word)
	}

	return projected, len(projected) > 0
}

func decodedInputCannotContributeForGuard(guard *Guard, input string) bool {
	if len(guard.decodeProof.automaton.nodes) == 0 {
		return false
	}

	proof := decodeLiteralProof{filter: &guard.decodeProof}

	proof.states[0] = 1

	return guardtext.WalkFlatBase64Readings(input, proof.visit)
}

type decodeProofStates [maxAggregateAutomatonNodes / 64]uint64

type decodeLiteralProof struct {
	filter  *decodeLiteralFilter
	states  decodeProofStates
	matched decodeProofStates
}

func (proof *decodeLiteralProof) visit(readings []string) bool {
	var next decodeProofStates

	for _, reading := range readings {
		proof.advanceReading(reading, &next)
	}

	// 선택지 수가 아니라 기존 리터럴 오토마톤의 상태 수만큼만 보존한다.
	proof.states = next

	return !proof.mayContribute()
}

func (proof *decodeLiteralProof) advanceReading(text string, next *decodeProofStates) {
	normalized := guardtext.NormalizeView(text)
	proof.advance(decodeProofWordText(normalized), next)

	// raw 규칙의 대소문자 동치와 정규화의 confusable 치환 순서가 다를 수 있다.
	// 소문자화 후 정규화한 해석도 더해 증명 범위를 보수적으로 넓힌다.
	lower := strings.ToLower(text)
	if lower == text {
		return
	}

	lowerNormalized := guardtext.NormalizeView(lower)
	if lowerNormalized != normalized {
		proof.advance(decodeProofWordText(lowerNormalized), next)
	}
}

func (proof *decodeLiteralProof) advance(text string, next *decodeProofStates) {
	for wordIndex, word := range proof.states {
		for word != 0 {
			state := wordIndex*64 + bits.TrailingZeros64(word)

			word &= word - 1

			for index := range len(text) {
				transition := proof.filter.automaton.nodes[state].next[text[index]]

				state = int(transition & aggregateAutomatonStateMask)

				if transition&aggregateAutomatonOutput != 0 {
					proof.recordMatches(aggregateAutomatonState(state))
				}
			}

			next[state/64] |= uint64(1) << (state % 64)
		}
	}
}

func (proof *decodeLiteralProof) recordMatches(state aggregateAutomatonState) {
	for state != 0 {
		proof.matched[state/64] |= uint64(1) << (state % 64)

		state = proof.filter.automaton.nodes[state].fail
	}
}

func (proof *decodeLiteralProof) mayContribute() bool {
	for _, groups := range proof.filter.rules {
		complete := true

		for _, group := range groups {
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

func (proof *decodeLiteralProof) groupMatched(group []aggregateAutomatonState) bool {
	for _, state := range group {
		if proof.matched[state/64]&(uint64(1)<<(state%64)) != 0 {
			return true
		}
	}

	return false
}

func decodeProofWordText(text string) string {
	return strings.Map(func(value rune) rune {
		if unicode.IsLetter(value) || unicode.IsNumber(value) || unicode.IsMark(value) {
			return value
		}

		return -1
	}, text)
}
