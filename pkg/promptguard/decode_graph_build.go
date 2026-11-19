package promptguard

import (
	"fmt"
	"math"
	"regexp"
	"regexp/syntax"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

type compiledDecodePattern struct {
	program     *syntax.Prog
	start       uint32
	first       []uint32
	firstASCII  [2]uint64
	firstHangul []uint64
	matchEmpty  bool
	maxRunes    int
}

const (
	hangulSyllableFirst rune = 0xAC00
	hangulSyllableLast  rune = 0xD7A3
)

func compileDecodePrograms(rule *compiledRule) ([]compiledDecodePattern, error) {
	var patterns []string

	if rule.Type == ruleTypeRegex {
		patterns = []string{rule.Pattern.String()}
	} else {
		patterns = make([]string, 0, len(rule.Phrases))
		for _, phrase := range rule.Phrases {
			pattern := regexp.QuoteMeta(phrase)

			if rule.MatchMode == phraseMatchToken {
				pattern = `(?:^|[^\p{L}\p{N}])` + pattern + `(?:$|[^\p{L}\p{N}])`
			}

			patterns = append(patterns, pattern)
		}
	}

	programs := make([]compiledDecodePattern, 0, len(patterns))
	for _, pattern := range patterns {
		parsed, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			return nil, fmt.Errorf("parse decode pattern: %w", err)
		}

		program, err := syntax.Compile(parsed.Simplify())
		if err != nil {
			return nil, fmt.Errorf("compile decode pattern: %w", err)
		}

		if program.Start < 0 || uint64(program.Start) > math.MaxUint32 {
			return nil, fmt.Errorf("decode pattern start out of range: %d", program.Start)
		}

		programs = append(programs, newCompiledDecodePattern(program, uint32(program.Start), regexMaxRunes(parsed)))
	}

	return programs, nil
}

func newCompiledDecodePattern(program *syntax.Prog, start uint32, maxRunes int) compiledDecodePattern {
	pattern := compiledDecodePattern{program: program, start: start, maxRunes: maxRunes}
	visited := make([]bool, len(program.Inst))
	stack := []uint32{pattern.start}

	for len(stack) > 0 {
		pc := stack[len(stack)-1]

		stack = stack[:len(stack)-1]

		if visited[pc] {
			continue
		}

		visited[pc] = true

		instruction := &program.Inst[pc]

		switch instruction.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			stack = append(stack, instruction.Out, instruction.Arg)
		case syntax.InstCapture, syntax.InstNop, syntax.InstEmptyWidth:
			stack = append(stack, instruction.Out)
		case syntax.InstMatch:
			pattern.matchEmpty = true
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			pattern.first = append(pattern.first, pc)

			for value := range rune(utf8.RuneSelf) {
				if decodeRuneMatches(instruction, value) {
					pattern.firstASCII[value>>6] |= 1 << (value & 63)
				}
			}

			pattern.addFirstHangul(instruction)
		case syntax.InstFail:
		}
	}

	return pattern
}

func (pattern *compiledDecodePattern) addFirstHangul(instruction *syntax.Inst) {
	var ranges []rune

	switch {
	case instruction.Op == syntax.InstRuneAny || instruction.Op == syntax.InstRuneAnyNotNL:
		ranges = []rune{hangulSyllableFirst, hangulSyllableLast}
	case len(instruction.Rune) == 1:
		ranges = []rune{instruction.Rune[0], instruction.Rune[0]}
	default:
		ranges = instruction.Rune
	}

	for index := 0; index+1 < len(ranges); index += 2 {
		low, high := max(ranges[index], hangulSyllableFirst), min(ranges[index+1], hangulSyllableLast)

		for value := low; value <= high; value++ {
			if pattern.firstHangul == nil {
				pattern.firstHangul = make([]uint64, (hangulSyllableLast-hangulSyllableFirst)/64+1)
			}

			offset := value - hangulSyllableFirst

			pattern.firstHangul[offset>>6] |= 1 << (offset & 63)
		}
	}
}

func (pattern *compiledDecodePattern) startMayAccept(value rune) bool {
	if pattern.matchEmpty {
		return true
	}

	if value < 0 {
		return false
	}

	if value < utf8.RuneSelf {
		return pattern.firstASCII[value>>6]&(1<<(value&63)) != 0
	}

	if value >= hangulSyllableFirst && value <= hangulSyllableLast {
		offset := value - hangulSyllableFirst

		return len(pattern.firstHangul) != 0 && pattern.firstHangul[offset>>6]&(1<<(offset&63)) != 0
	}

	for _, pc := range pattern.first {
		if decodeRuneMatches(&pattern.program.Inst[pc], value) {
			return true
		}
	}

	return false
}

type decodeSourcePart struct {
	start, end int
	values     []string
	raws       []string
	fragments  []string
}

func (part *decodeSourcePart) viewTexts(view string) []string {
	if part.raws == nil {
		part.raws = make([]string, len(part.values))
		for index, value := range part.values {
			part.raws[index] = guardtext.ComposeJamoSequences(guardtext.SanitizeUTF8(value))
		}
	}

	if view == viewRaw {
		return part.raws
	}

	if part.fragments == nil {
		part.fragments = make([]string, len(part.raws))
		for index, raw := range part.raws {
			part.fragments[index] = guardtext.NormalizeFragment(raw)
		}
	}

	return part.fragments
}

func decodeSourceParts(input string, regions []guardtext.RuleDecodeRegion) []decodeSourcePart {
	parts := make([]decodeSourcePart, 0, 2*len(regions)+1)
	position := 0

	for _, region := range regions {
		if region.Start > position {
			parts = append(parts, decodeSourcePart{start: position, end: region.Start, values: []string{input[position:region.Start]}})
		}

		values := make([]string, 1, len(region.Readings)+1)

		values[0] = input[region.Start:region.End]
		values = append(values, region.Readings...)
		parts = append(parts, decodeSourcePart{start: region.Start, end: region.End, values: values})
		position = region.End
	}

	parts = append(parts, decodeSourcePart{start: position, end: len(input), values: []string{input[position:]}})

	return parts
}

type decodeViewKey struct {
	state    decodeViewState
	previous rune
}

func buildDecodeViewGraph(parts []decodeSourcePart, view string) decodeViewGraph {
	graph := decodeViewGraph{layers: [][]decodeGraphNode{{{previous: -1}}}}
	current := []decodeViewKey{{previous: -1}}

	for partIndex := range parts {
		part := &parts[partIndex]
		texts := part.viewTexts(view)

		var next []decodeViewKey

		nextIndex := make(map[decodeViewKey]int)
		layer := graph.layers[len(graph.layers)-1]

		for index, key := range current {
			for readingIndex, value := range part.values {
				text, state := graphViewText(texts[readingIndex], view, key.state)
				previous := key.previous

				if text != "" {
					last, _ := utf8.DecodeLastRuneInString(text)

					previous = decodePreviousClass(last)
				}

				target := decodeViewKey{state: state, previous: previous}
				to, exists := nextIndex[target]

				if !exists {
					to = len(next)
					nextIndex[target] = to
					next = append(next, target)
				}

				edge := decodeGraphEdge{to: to, text: text, start: part.start, end: part.end}

				if readingIndex > 0 {
					edge.selection = &decodeSelection{start: part.start, end: part.end, value: value}
				}

				layer[index].edges = append(layer[index].edges, edge)
			}
		}

		nodes := make([]decodeGraphNode, len(next))
		for index, key := range next {
			nodes[index].previous = key.previous
		}

		graph.layers = append(graph.layers, nodes)
		current = next
	}

	return graph
}

func decodePreviousClass(value rune) rune {
	if value == '\n' || value < 0 {
		return value
	}

	if syntax.IsWordChar(value) {
		return 'a'
	}

	return '!'
}
