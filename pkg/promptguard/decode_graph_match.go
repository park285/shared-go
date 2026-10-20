package promptguard

import (
	"regexp/syntax"
	"slices"
	"strings"
	"unicode/utf8"
)

type decodeSelection struct {
	start, end int
	value      string
}

type decodeSelectionPath struct {
	previous  *decodeSelectionPath
	selection *decodeSelection
}

type decodeGraphEdge struct {
	to        int
	text      string
	start     int
	end       int
	selection *decodeSelection
}

type decodeGraphNode struct {
	previous rune
	edges    []decodeGraphEdge
}

type decodeViewGraph struct {
	layers [][]decodeGraphNode
}

type decodeMatchThread struct {
	pc         uint32
	start, end int
	path       *decodeSelectionPath
}

type decodeThreadSet struct {
	threads []decodeMatchThread
	indices []int
}

func (set *decodeThreadSet) reset(size int) {
	if len(set.indices) == 0 {
		set.indices = make([]int, size)
	}

	for _, thread := range set.threads {
		set.indices[thread.pc] = 0
	}

	set.threads = set.threads[:0]
}

func (set *decodeThreadSet) add(thread decodeMatchThread) {
	if set.indices[thread.pc] != 0 {
		return
	}

	set.threads = append(set.threads, thread)
	set.indices[thread.pc] = len(set.threads)
}

type decodeNodeRun struct {
	set     decodeThreadSet
	prefix  *decodeSelectionPath
	reached bool
}

type decodeGraphMatcher struct {
	program    *syntax.Prog
	pattern    *compiledDecodePattern
	start      uint32
	current    []decodeNodeRun
	next       []decodeNodeRun
	left       decodeThreadSet
	right      decodeThreadSet
	visited    []uint32
	generation uint32
	stack      []decodeMatchThread
	emit       func(decodeMatchThread) bool
	done       bool
}

func matchDecodeGraph(graph decodeViewGraph, pattern *compiledDecodePattern, emit func(decodeMatchThread) bool) {
	program := pattern.program
	matcher := decodeGraphMatcher{program: program, pattern: pattern, start: pattern.start, visited: make([]uint32, len(program.Inst)), emit: emit}

	matcher.current = resizeDecodeRuns(nil, len(graph.layers[0]), len(program.Inst))
	matcher.current[0].reached = true
	matcher.left.reset(len(program.Inst))
	matcher.right.reset(len(program.Inst))

	for layerIndex, layer := range graph.layers {
		if matcher.done {
			return
		}

		if layerIndex == len(graph.layers)-1 {
			for nodeIndex, node := range layer {
				run := &matcher.current[nodeIndex]
				if run.reached {
					matcher.step(&run.set, &matcher.right, run.prefix, node.previous, -1, 0, 0, true)
				}
			}

			return
		}

		matcher.next = resizeDecodeRuns(matcher.next, len(graph.layers[layerIndex+1]), len(program.Inst))

		for nodeIndex, node := range layer {
			run := &matcher.current[nodeIndex]
			if !run.reached {
				continue
			}

			for _, edge := range node.edges {
				matcher.advance(run, node.previous, edge)

				if matcher.done {
					return
				}
			}
		}

		matcher.current, matcher.next = matcher.next, matcher.current
	}
}

func resizeDecodeRuns(runs []decodeNodeRun, count, instructions int) []decodeNodeRun {
	if count > cap(runs) {
		runs = append(runs[:cap(runs)], make([]decodeNodeRun, count-cap(runs))...)
	} else {
		runs = runs[:count]
	}

	for index := range runs {
		runs[index].set.reset(instructions)

		runs[index].prefix = nil
		runs[index].reached = false
	}

	return runs
}

func (matcher *decodeGraphMatcher) advance(run *decodeNodeRun, previous rune, edge decodeGraphEdge) {
	matcher.left.reset(len(matcher.program.Inst))

	var paths map[*decodeSelectionPath]*decodeSelectionPath

	selectPath := func(path *decodeSelectionPath) *decodeSelectionPath {
		if edge.selection == nil {
			return path
		}

		if selected, exists := paths[path]; exists {
			return selected
		}

		if paths == nil {
			paths = make(map[*decodeSelectionPath]*decodeSelectionPath)
		}

		selected := &decodeSelectionPath{previous: path, selection: edge.selection}

		paths[path] = selected

		return selected
	}

	prefix := selectPath(run.prefix)
	for _, thread := range run.set.threads {
		thread.path = selectPath(thread.path)
		matcher.left.add(thread)
	}

	// 해석 선택이 없는 경로에서 새로 시작한 스레드는 해석 간선을 지나기 전까지 경로가 없어 일치를 내도 observeGraph가
	// 버린다. 정규식은 최대 maxRunes rune만 읽으므로 간선 끝에서 그보다 앞에서 시작한 스레드는 다음 간선에 닿지 못한다.
	startFrom := 0

	if prefix == nil && matcher.pattern.maxRunes >= 0 {
		startFrom = utf8.RuneCountInString(edge.text) - matcher.pattern.maxRunes
	}

	index := 0

	for _, value := range edge.text {
		startThread := index >= startFrom
		index++

		// 살아 있는 스레드가 없고 새 스레드도 시작하지 않으면 step은 빈 집합만 맞바꾸므로 부르지 않는다.
		if len(matcher.left.threads) == 0 && (!startThread || !matcher.pattern.startMayAccept(value)) {
			previous = value

			continue
		}

		matcher.step(&matcher.left, &matcher.right, prefix, previous, value, edge.start, edge.end, startThread)

		matcher.left, matcher.right = matcher.right, matcher.left
		previous = value

		if matcher.done {
			return
		}
	}

	target := &matcher.next[edge.to]
	if !target.reached {
		target.reached = true
		target.prefix = prefix
	}

	for _, thread := range matcher.left.threads {
		target.set.add(thread)
	}
}

func (matcher *decodeGraphMatcher) step(
	current, next *decodeThreadSet,
	prefix *decodeSelectionPath,
	previous, value rune,
	start, end int,
	startThread bool,
) {
	next.reset(len(matcher.program.Inst))

	matcher.generation++
	if matcher.generation == 0 {
		clear(matcher.visited)

		matcher.generation++
	}

	for _, thread := range current.threads {
		matcher.closure(thread, next, previous, value, end)
	}

	if startThread && matcher.pattern.startMayAccept(value) {
		matcher.closure(decodeMatchThread{pc: matcher.start, start: start, end: start, path: prefix}, next, previous, value, end)
	}
}

func (matcher *decodeGraphMatcher) closure(initial decodeMatchThread, next *decodeThreadSet, previous, value rune, end int) {
	matcher.stack = append(matcher.stack[:0], initial)
	for len(matcher.stack) > 0 && !matcher.done {
		last := len(matcher.stack) - 1
		thread := matcher.stack[last]

		matcher.stack = matcher.stack[:last]

		if matcher.visited[thread.pc] == matcher.generation {
			continue
		}

		matcher.visited[thread.pc] = matcher.generation

		instruction := &matcher.program.Inst[thread.pc]

		switch instruction.Op {
		case syntax.InstFail:
		case syntax.InstAlt, syntax.InstAltMatch:
			alternate := thread

			alternate.pc = instruction.Arg
			thread.pc = instruction.Out
			matcher.stack = append(matcher.stack, alternate, thread)
		case syntax.InstCapture, syntax.InstNop:
			thread.pc = instruction.Out
			matcher.stack = append(matcher.stack, thread)
		case syntax.InstEmptyWidth:
			if instruction.MatchEmptyWidth(previous, value) {
				thread.pc = instruction.Out
				matcher.stack = append(matcher.stack, thread)
			}
		case syntax.InstMatch:
			matcher.done = matcher.emit(thread)
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			if value >= 0 && decodeRuneMatches(instruction, value) {
				thread.pc = instruction.Out
				thread.end = end
				next.add(thread)
			}
		}
	}
}

func decodeRuneMatches(instruction *syntax.Inst, value rune) bool {
	if instruction.Op == syntax.InstRuneAny {
		return true
	}

	if instruction.Op == syntax.InstRuneAnyNotNL {
		return value != '\n'
	}

	return instruction.MatchRune(value)
}

func materializeDecodePath(input string, path, confirmed *decodeSelectionPath, start int) (string, *decodeSelectionPath) {
	var selections []*decodeSelection

	for current := path; current != nil; current = current.previous {
		selections = append(selections, current.selection)
	}

	for current := confirmed; current != nil; current = current.previous {
		if current.selection.end <= start {
			selections = append(selections, current.selection)
		}
	}

	slices.SortStableFunc(selections, func(left, right *decodeSelection) int { return left.start - right.start })

	var output strings.Builder

	output.Grow(len(input))

	position := 0

	var combined *decodeSelectionPath

	for _, selection := range selections {
		if selection.start < position {
			continue
		}

		output.WriteString(input[position:selection.start])
		output.WriteString(selection.value)

		position = selection.end
		combined = &decodeSelectionPath{previous: combined, selection: selection}
	}

	output.WriteString(input[position:])

	return output.String(), combined
}
