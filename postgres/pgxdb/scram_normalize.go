package pgxdb

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type scramCharacter struct {
	codepoint rune
	ccc       uint8
}

func postgresNFKC(mapped string) string {
	normalized := norm.NFKC.String(mapped)
	if !strings.ContainsRune(normalized, '\u034f') {
		return normalized
	}

	decomposed := norm.NFKD.String(mapped)
	chars := make([]scramCharacter, 0, utf8.RuneCountInString(decomposed))

	for offset, r := range decomposed {
		if r != '\u034f' {
			chars = append(chars, scramCharacter{r, norm.NFD.PropertiesString(decomposed[offset:]).CCC()})
		}
	}

	orderSCRAMCharacters(chars)

	size := composeSCRAMCharacters(chars)

	var result strings.Builder

	result.Grow(len(decomposed))

	for _, ch := range chars[:size] {
		result.WriteRune(ch.codepoint)
	}

	return result.String()
}

func orderSCRAMCharacters(chars []scramCharacter) {
	for start := 0; start < len(chars); {
		end := start + 1
		for end < len(chars) && chars[end].ccc != 0 {
			end++
		}

		if chars[start].ccc == 0 {
			start++
		}

		slices.SortStableFunc(chars[start:end], func(a, b scramCharacter) int {
			return cmp.Compare(a.ccc, b.ccc)
		})

		start = end
	}
}

func composeSCRAMCharacters(chars []scramCharacter) int {
	starter, size, lastClass := 0, 1, -1

	var pair, composed [2 * utf8.UTFMax]byte

	for _, ch := range chars[1:] {
		if lastClass < int(ch.ccc) {
			input := utf8.AppendRune(pair[:0], chars[starter].codepoint)

			input = utf8.AppendRune(input, ch.codepoint)

			output := norm.NFC.Append(composed[:0], input...)
			r, width := utf8.DecodeRune(output)

			if width == len(output) {
				chars[starter].codepoint = r
				continue
			}
		}

		if ch.ccc == 0 {
			starter = size
			lastClass = -1
		} else {
			lastClass = int(ch.ccc)
		}

		chars[size] = ch
		size++
	}

	return size
}
