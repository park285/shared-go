package outputguard

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/park285/shared-go/pkg/internal/guardtext"
)

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
	runes        []rune
	tokens       []tokenSpan
	runeFallback bool
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
}

func validateProtectedTexts(input []string) ([]string, bool) {
	protected := make([]string, 0, min(len(input), maxProtectedTexts))
	totalBytes := 0
	for _, text := range slices.Clone(input) {
		if len(text) > maxProtectedTextBytes {
			return nil, false
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if len(protected) == maxProtectedTexts {
			return nil, false
		}
		totalBytes += len(text)
		if totalBytes > maxProtectedTotalBytes {
			return nil, false
		}
		protected = append(protected, text)
	}

	return protected, true
}

func buildProtectedIndex(protectedTexts []string) *protectedIndex {
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

		entry := protectedEntry{runes: []rune(normalized)}
		entry.tokens = tokenizeRunes(entry.runes)
		entry.runeFallback = len(entry.tokens) <= 1
		entryIndex := len(index.entries)
		index.entries = append(index.entries, entry)
		index.addTokenAnchors(entryIndex)
		index.addRuneAnchors(entryIndex)
	}

	return index
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

func (index *protectedIndex) overlaps(output []rune) bool {
	if index == nil || len(output) == 0 {
		return false
	}
	outputTokens := tokenizeRunes(output)
	if index.hasTokenOverlap(output, outputTokens) {
		return true
	}

	return index.hasRuneOverlap(output, outputTokens)
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
