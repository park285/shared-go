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

type preparedProtectedText struct {
	normalized string
	exact      string
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

type exactNode uint64

const (
	exactFirstBits      = 19
	exactFirstMask      = uint64(1<<exactFirstBits - 1)
	exactCountBits      = 9
	exactCountMask      = uint64(1<<exactCountBits - 1)
	exactCountShift     = exactFirstBits
	exactTerminalShift  = exactCountShift + exactCountBits
	exactFailureShift   = exactTerminalShift + 1
	exactFailureBits    = 19
	exactFailureMask    = uint64(1<<exactFailureBits - 1)
	exactIncomingShift  = exactFailureShift + exactFailureBits
	exactFailureReady   = uint64(1 << 56)
	exactTransitionMask = uint64(1<<exactFailureShift - 1)
)

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
	if len(protected) == 0 {
		return nil, false, false
	}
	index, err := newProtectedIndex(protected)
	return index, err != nil, false
}

func newProtectedIndex(protectedTexts []string) (*protectedIndex, error) {
	prepared := prepareProtectedTexts(protectedTexts)
	patterns := protectedExactPatterns(prepared)
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
	return buildProtectedIndexWithPatterns(prepared, patterns), nil
}

func buildProtectedIndex(protectedTexts []string) *protectedIndex {
	prepared := prepareProtectedTexts(protectedTexts)
	return buildProtectedIndexWithPatterns(prepared, protectedExactPatterns(prepared))
}

func buildProtectedIndexWithPatterns(protectedTexts []preparedProtectedText, exactPatterns []string) *protectedIndex {
	index := &protectedIndex{
		entries:      make([]protectedEntry, 0, len(protectedTexts)),
		tokenAnchors: make(map[uint64][]anchorRef),
		runeAnchors:  make(map[uint64][]anchorRef),
	}
	for _, text := range protectedTexts {
		entry := protectedEntry{normalized: text.normalized, runes: []rune(text.normalized)}
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

func prepareProtectedTexts(protectedTexts []string) []preparedProtectedText {
	prepared := make([]preparedProtectedText, 0, len(protectedTexts))
	seen := make(map[string]struct{}, len(protectedTexts))
	for _, text := range protectedTexts {
		normalized := guardtext.NormalizeViews(text).Norm
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		prepared = append(prepared, preparedProtectedText{
			normalized: normalized,
			exact:      exactProtectedProjection(text),
		})
	}
	return prepared
}

func exactProtectedProjection(text string) string {
	stripped := guardtext.StripFormatAndCombining(text)
	return guardtext.NormalizeViews(stripped).Joined
}

func protectedExactPatterns(protectedTexts []preparedProtectedText) []string {
	patterns := make([]string, 0, len(protectedTexts))
	for _, text := range protectedTexts {
		patterns = append(patterns, text.exact)
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
	if len(patterns) == 0 {
		return nil
	}
	slices.Sort(patterns)
	totalBytes := 0
	for _, pattern := range patterns {
		totalBytes += len(pattern)
	}
	if totalBytes > maxProtectedTotalBytes {
		return nil
	}
	nodes := make([]exactNode, 1, totalBytes+1)
	buildExactTrieNode(&nodes, 0, patterns, 0)
	buildExactFailures(nodes)

	return nodes
}

func buildExactTrieNode(nodes *[]exactNode, node uint32, patterns []string, depth int) {
	if len(patterns) == 1 {
		pattern := patterns[0]
		for depth < len(pattern) {
			child := uint32(len(*nodes))
			setExactTransitions(&(*nodes)[node], int(child), 1, false)
			*nodes = append(*nodes, packExactParent(node, pattern[depth]))
			node = child
			depth++
		}
		setExactTransitions(&(*nodes)[node], 0, 0, true)
		return
	}

	remaining := 0
	for remaining < len(patterns) && len(patterns[remaining]) == depth {
		remaining++
	}
	terminal := remaining > 0
	patterns = patterns[remaining:]
	childCount := 0
	for start := 0; start < len(patterns); {
		childCount++
		value := patterns[start][depth]
		start++
		for start < len(patterns) && patterns[start][depth] == value {
			start++
		}
	}
	first := len(*nodes)
	for range childCount {
		*nodes = append(*nodes, 0)
	}
	setExactTransitions(&(*nodes)[node], first, childCount, terminal)

	childOffset := 0
	for start := 0; start < len(patterns); {
		end := start + 1
		value := patterns[start][depth]
		for end < len(patterns) && patterns[end][depth] == value {
			end++
		}
		child := uint32(first + childOffset)
		(*nodes)[child] = packExactParent(node, value)
		childOffset++
		buildExactTrieNode(nodes, child, patterns[start:end], depth+1)
		start = end
	}
}

func buildExactFailures(nodes []exactNode) {
	nodes[0] |= exactNode(exactFailureReady)
	for node := uint32(1); node < uint32(len(nodes)); node++ {
		resolveExactFailure(nodes, node)
	}
}

func resolveExactFailure(nodes []exactNode, node uint32) uint32 {
	packed := nodes[node]
	if uint64(packed)&exactFailureReady != 0 {
		return exactFailure(nodes[node])
	}
	parent := exactFailure(nodes[node])
	value := exactIncoming(nodes[node])
	if parent == 0 {
		setExactFailure(&nodes[node], 0)
		return 0
	}

	fallback := resolveExactFailure(nodes, parent)
	for {
		next, ok := exactNext(nodes, fallback, value)
		if ok && next != node {
			fallback = next
			break
		}
		if fallback == 0 {
			break
		}
		fallback = resolveExactFailure(nodes, fallback)
	}
	resolveExactFailure(nodes, fallback)
	setExactFailure(&nodes[node], fallback)
	if exactTerminal(nodes[fallback]) {
		nodes[node] |= 1 << exactTerminalShift
	}
	return fallback
}

func setExactTransitions(node *exactNode, first, count int, terminal bool) {
	if count == 0 {
		first = 0
	}
	packed := uint64(first) | uint64(count)<<exactCountShift
	if terminal {
		packed |= 1 << exactTerminalShift
	}
	*node = exactNode(uint64(*node)&^exactTransitionMask | packed)
}

func packExactParent(parent uint32, value byte) exactNode {
	return exactNode(uint64(parent)<<exactFailureShift | uint64(value)<<exactIncomingShift)
}

func setExactFailure(node *exactNode, failure uint32) {
	const failureFieldMask = exactFailureMask << exactFailureShift
	*node = exactNode(uint64(*node)&^failureFieldMask | uint64(failure)<<exactFailureShift | exactFailureReady)
}

func exactFirst(node exactNode) uint32 {
	return uint32(uint64(node) & exactFirstMask)
}

func exactCount(node exactNode) int {
	return int(uint64(node) >> exactCountShift & exactCountMask)
}

func exactTerminal(node exactNode) bool {
	return uint64(node)&(1<<exactTerminalShift) != 0
}

func exactFailure(node exactNode) uint32 {
	return uint32(uint64(node) >> exactFailureShift & exactFailureMask)
}

func exactIncoming(node exactNode) byte {
	return byte(uint64(node) >> exactIncomingShift)
}

func exactNext(nodes []exactNode, state uint32, value byte) (uint32, bool) {
	node := nodes[state]
	first := exactFirst(node)
	count := exactCount(node)
	if count == 1 {
		if exactIncoming(nodes[first]) == value {
			return first, true
		}
		return 0, false
	}
	low, high := 0, count
	for low < high {
		middle := int(uint(low+high) >> 1)
		if exactIncoming(nodes[first+uint32(middle)]) < value {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low < count && exactIncoming(nodes[first+uint32(low)]) == value {
		return first + uint32(low), true
	}
	return 0, false
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
	state := uint32(0)
	for _, b := range []byte(text) {
		next, ok := exactNext(index.exactNodes, state, b)
		for !ok && state != 0 {
			state = exactFailure(index.exactNodes[state])
			next, ok = exactNext(index.exactNodes, state, b)
		}
		if ok {
			state = next
		}
		if exactTerminal(index.exactNodes[state]) {
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
