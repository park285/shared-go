package outputguard

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/park285/shared-go/pkg/internal/guardtext"
)

var ErrInvalidProtectedTexts = errors.New("outputguard: invalid protected texts")

const (
	maxProtectedTexts      = 16
	maxProtectedTextBytes  = 64 << 10
	maxProtectedTotalBytes = 256 << 10
	protectedTokenWindow   = 12
	protectedMinRunes      = 80
	protectedRuneWindow    = 96
	tokenAnchorWindow      = protectedTokenWindow / 2
	runeAnchorWindow       = protectedRuneWindow / 2
	rollingHashBase        = uint64(1099511628211)
)

type tokenSpan struct {
	start int
	end   int
}

type protectedEntry struct {
	normalized   string
	runes        []rune
	tokens       []tokenSpan
	runeFallback bool
}

type normalizedSurface struct {
	text  string
	runes []rune
}

type anchorRef struct {
	entry int
	start int
	end   int
}

type protectedIndex struct {
	entries      []protectedEntry
	tokenAnchors map[uint64][]anchorRef
	runeAnchors  map[uint64][]anchorRef
	exactNodes   []exactNode
}

type exactNode struct {
	edges    []exactEdge
	fail     int
	terminal bool
}
type exactEdge struct {
	byte byte
	next int
}

func validateProtectedTexts(input []string) ([]string, bool, bool) {
	protected := make([]string, 0, min(len(input), maxProtectedTexts))
	totalBytes := 0
	for _, text := range slices.Clone(input) {
		if len(text) > maxProtectedTextBytes {
			return nil, false, true
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if len(protected) == maxProtectedTexts {
			return nil, false, true
		}
		totalBytes += len(text)
		if totalBytes > maxProtectedTotalBytes {
			return nil, false, true
		}
		protected = append(protected, text)
	}

	return protected, true, false
}

func buildCompatibilityIndex(input []string) (*protectedIndex, bool, bool) {
	protected, ok, oversize := validateProtectedTexts(input)
	if !ok {
		return nil, false, oversize
	}
	index, err := newProtectedIndex(protected)
	return index, err != nil, false
}

func newProtectedIndex(protectedTexts []string) (*protectedIndex, error) {
	patterns := protectedExactPatterns(protectedTexts)
	normalizedTotal := 0
	for _, pattern := range patterns {
		if len([]rune(pattern)) < 8 {
			return nil, ErrInvalidProtectedTexts
		}
		if len(pattern) > maxProtectedTextBytes {
			return nil, ErrInvalidProtectedTexts
		}
		normalizedTotal += len(pattern)
		if normalizedTotal > maxProtectedTotalBytes {
			return nil, ErrInvalidProtectedTexts
		}
	}
	return buildProtectedIndexWithPatterns(protectedTexts, patterns), nil
}

func buildProtectedIndex(protectedTexts []string) *protectedIndex {
	return buildProtectedIndexWithPatterns(protectedTexts, protectedExactPatterns(protectedTexts))
}

func buildProtectedIndexWithPatterns(protectedTexts, exactPatterns []string) *protectedIndex {
	index := &protectedIndex{
		entries:      make([]protectedEntry, 0, len(protectedTexts)),
		tokenAnchors: make(map[uint64][]anchorRef),
		runeAnchors:  make(map[uint64][]anchorRef),
	}
	seen := make(map[string]struct{}, len(protectedTexts))
	for _, text := range slices.Clone(protectedTexts) {
		normalized := guardtext.Normalize(text)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		entry := protectedEntry{normalized: normalized, runes: []rune(normalized)}
		entry.tokens = tokenizeRunes(entry.runes)
		entry.runeFallback = len(entry.tokens) <= 1
		entryIndex := len(index.entries)
		index.entries = append(index.entries, entry)
		index.addTokenAnchors(entryIndex)
		index.addRuneAnchors(entryIndex)
	}
	index.exactNodes = buildExactMatcher(exactPatterns)

	return index
}

func protectedExactPatterns(protectedTexts []string) []string {
	patterns := make([]string, 0, len(protectedTexts)*2)
	for _, text := range protectedTexts {
		views := guardtext.NormalizeViews(text)
		stripped := guardtext.StripFormatAndCombining(text)
		patterns = append(patterns, views.Norm, guardtext.Normalize(views.Joined), guardtext.Normalize(stripped))
	}
	return compactProtectedPatterns(patterns)
}

func compactProtectedPatterns(patterns []string) []string {
	seen := make(map[string]struct{}, len(patterns))
	compacted := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		compacted = append(compacted, pattern)
	}
	return compacted
}

func buildExactMatcher(patterns []string) []exactNode {
	children := []map[byte]int{{}}
	terminals := []bool{false}
	for _, pattern := range patterns {
		state := 0
		for _, b := range []byte(pattern) {
			next, ok := children[state][b]
			if !ok {
				next = len(children)
				children[state][b] = next
				children = append(children, map[byte]int{})
				terminals = append(terminals, false)
			}
			state = next
		}
		terminals[state] = true
	}
	nodes := make([]exactNode, len(children))
	for i, childMap := range children {
		nodes[i].terminal = terminals[i]
		for b, next := range childMap {
			nodes[i].edges = append(nodes[i].edges, exactEdge{byte: b, next: next})
		}
		sort.Slice(nodes[i].edges, func(a, b int) bool { return nodes[i].edges[a].byte < nodes[i].edges[b].byte })
	}
	queue := make([]int, 0, len(nodes))
	for _, edge := range nodes[0].edges {
		queue = append(queue, edge.next)
	}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for _, edge := range nodes[state].edges {
			fallback := nodes[state].fail
			for fallback != 0 && exactNext(nodes[fallback].edges, edge.byte) < 0 {
				fallback = nodes[fallback].fail
			}
			if next := exactNext(nodes[fallback].edges, edge.byte); next >= 0 {
				nodes[edge.next].fail = next
			}
			nodes[edge.next].terminal = nodes[edge.next].terminal || nodes[nodes[edge.next].fail].terminal
			queue = append(queue, edge.next)
		}
	}
	return nodes
}

func exactNext(edges []exactEdge, b byte) int {
	i := sort.Search(len(edges), func(i int) bool { return edges[i].byte >= b })
	if i < len(edges) && edges[i].byte == b {
		return edges[i].next
	}
	return -1
}

func (index *protectedIndex) addTokenAnchors(entryIndex int) {
	entry := &index.entries[entryIndex]
	for tokenStart := 0; tokenStart+tokenAnchorWindow <= len(entry.tokens); tokenStart += tokenAnchorWindow {
		start := entry.tokens[tokenStart].start
		end := entry.tokens[tokenStart+tokenAnchorWindow-1].end
		hash := hashRunes(entry.runes[start:end])
		index.tokenAnchors[hash] = append(index.tokenAnchors[hash], anchorRef{entry: entryIndex, start: start, end: end})
	}
}

func (index *protectedIndex) addRuneAnchors(entryIndex int) {
	entry := &index.entries[entryIndex]
	if !entry.runeFallback {
		return
	}
	for start := 0; start+runeAnchorWindow <= len(entry.runes); start += runeAnchorWindow {
		end := start + runeAnchorWindow
		hash := hashRunes(entry.runes[start:end])
		index.runeAnchors[hash] = append(index.runeAnchors[hash], anchorRef{entry: entryIndex, start: start, end: end})
	}
}

func newNormalizedSurface(text string) normalizedSurface {
	normalized := guardtext.Normalize(text)

	return normalizedSurface{text: normalized, runes: []rune(normalized)}
}

func (index *protectedIndex) overlapsText(text string) bool {
	return index.overlaps(newNormalizedSurface(text))
}

func (index *protectedIndex) overlaps(surface normalizedSurface) bool {
	if index == nil || surface.text == "" {
		return false
	}
	if index.exactContains(surface.text) {
		return true
	}

	output := surface.runes
	outputTokens := tokenizeRunes(output)
	if index.hasTokenOverlap(output, outputTokens) {
		return true
	}

	return index.hasRuneOverlap(output, outputTokens)
}

func (index *protectedIndex) exactContains(text string) bool {
	if len(index.exactNodes) == 0 {
		return false
	}
	state := 0
	for _, b := range []byte(text) {
		for state != 0 && exactNext(index.exactNodes[state].edges, b) < 0 {
			state = index.exactNodes[state].fail
		}
		if next := exactNext(index.exactNodes[state].edges, b); next >= 0 {
			state = next
		}
		if index.exactNodes[state].terminal {
			return true
		}
	}
	return false
}

func (index *protectedIndex) hasTokenOverlap(output []rune, outputTokens []tokenSpan) bool {
	for tokenStart := 0; tokenStart+tokenAnchorWindow <= len(outputTokens); tokenStart++ {
		start := outputTokens[tokenStart].start
		end := outputTokens[tokenStart+tokenAnchorWindow-1].end
		refs := index.tokenAnchors[hashRunes(output[start:end])]
		for _, ref := range refs {
			entry := &index.entries[ref.entry]
			if !slices.Equal(output[start:end], entry.runes[ref.start:ref.end]) {
				continue
			}
			outputStart, outputEnd, _, _ := expandExactRunes(output, start, end, entry.runes, ref.start, ref.end)
			if outputEnd-outputStart >= protectedMinRunes && hasTokenCountInRange(outputTokens, outputStart, outputEnd, protectedTokenWindow) {
				return true
			}
		}
	}

	return false
}

func (index *protectedIndex) hasRuneOverlap(output []rune, outputTokens []tokenSpan) bool {
	if len(output) < runeAnchorWindow || len(index.runeAnchors) == 0 {
		return false
	}
	hash := hashRunes(output[:runeAnchorWindow])
	power := rollingHashPower(runeAnchorWindow - 1)
	for start := 0; start+runeAnchorWindow <= len(output); start++ {
		if start > 0 {
			hash = rollRuneHash(hash, output[start-1], output[start+runeAnchorWindow-1], power)
		}
		end := start + runeAnchorWindow
		for _, ref := range index.runeAnchors[hash] {
			entry := &index.entries[ref.entry]
			if !slices.Equal(output[start:end], entry.runes[ref.start:ref.end]) {
				continue
			}
			outputStart, outputEnd, _, _ := expandExactRunes(output, start, end, entry.runes, ref.start, ref.end)
			if outputEnd-outputStart >= protectedRuneWindow {
				return true
			}
		}
	}

	return false
}

func tokenizeRunes(text []rune) []tokenSpan {
	tokens := make([]tokenSpan, 0, len(text)/4)
	for i := 0; i < len(text); {
		if !isTokenRune(text[i]) {
			i++
			continue
		}
		start := i
		for i < len(text) && isTokenRune(text[i]) {
			i++
		}
		tokens = append(tokens, tokenSpan{start: start, end: i})
	}

	return tokens
}

func isTokenRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
}

func expandExactRunes(left []rune, leftStart, leftEnd int, right []rune, rightStart, rightEnd int) (int, int, int, int) {
	for leftStart > 0 && rightStart > 0 && left[leftStart-1] == right[rightStart-1] {
		leftStart--
		rightStart--
	}
	for leftEnd < len(left) && rightEnd < len(right) && left[leftEnd] == right[rightEnd] {
		leftEnd++
		rightEnd++
	}

	return leftStart, leftEnd, rightStart, rightEnd
}

func hasTokenCountInRange(tokens []tokenSpan, start, end, minimum int) bool {
	first := sort.Search(len(tokens), func(i int) bool { return tokens[i].start >= start })
	count := 0
	for i := first; i < len(tokens) && tokens[i].end <= end; i++ {
		count++
		if count >= minimum {
			return true
		}
	}

	return false
}

func hashRunes(text []rune) uint64 {
	var hash uint64
	for _, r := range text {
		hash = hash*rollingHashBase + uint64(r) + 1
	}

	return hash
}

func rollingHashPower(exponent int) uint64 {
	power := uint64(1)
	for range exponent {
		power *= rollingHashBase
	}

	return power
}

func rollRuneHash(hash uint64, oldRune, newRune rune, power uint64) uint64 {
	return (hash-(uint64(oldRune)+1)*power)*rollingHashBase + uint64(newRune) + 1
}
