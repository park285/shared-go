package guardtext

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

//go:generate go run ./genconfusables.go -confusables-source ./testdata/confusables-17.0.0.txt.gz -unicode-data-baseline-source ./testdata/UnicodeData-15.0.0.txt.gz -unicode-data-source ./testdata/UnicodeData-17.0.0.txt.gz -output ./confusables_table_generated.go

func confusableSkeleton(value string) string {
	value = unicode17NFD(value)

	var mapped strings.Builder
	mapped.Grow(len(value))
	for _, current := range value {
		if replacement, ok := confusablesMap[current]; ok {
			mapped.WriteString(replacement)
			continue
		}
		mapped.WriteRune(current)
	}

	return unicode17NFD(mapped.String())
}

func unicode17NFD(value string) string {
	if !needsUnicode17NFDOverlay(value) {
		return norm.NFD.String(value)
	}

	decomposed := make([]rune, 0, len(value))
	for _, current := range value {
		if replacement, ok := unicode17CanonicalDecompositionDelta[current]; ok {
			decomposed = append(decomposed, []rune(replacement)...)
			continue
		}
		decomposed = append(decomposed, []rune(norm.NFD.String(string(current)))...)
	}

	for current := 1; current < len(decomposed); current++ {
		currentCCC := unicode17CanonicalCombiningClass(decomposed[current])
		if currentCCC == 0 {
			continue
		}
		for previous := current; previous > 0; previous-- {
			previousCCC := unicode17CanonicalCombiningClass(decomposed[previous-1])
			if previousCCC == 0 || previousCCC <= currentCCC {
				break
			}
			decomposed[previous-1], decomposed[previous] = decomposed[previous], decomposed[previous-1]
		}
	}

	return string(decomposed)
}

func needsUnicode17NFDOverlay(value string) bool {
	for offset := 0; offset < len(value); {
		if value[offset] < utf8.RuneSelf {
			offset++
			continue
		}
		current, size := utf8.DecodeRuneInString(value[offset:])
		if _, ok := unicode17CanonicalDecompositionDelta[current]; ok {
			return true
		}
		if _, ok := unicode17CanonicalCombiningClassDelta[current]; ok {
			return true
		}
		offset += size
	}

	return false
}

func unicode17CanonicalCombiningClass(value rune) uint8 {
	if canonicalCombiningClass, ok := unicode17CanonicalCombiningClassDelta[value]; ok {
		return canonicalCombiningClass
	}

	return norm.NFD.PropertiesString(string(value)).CCC()
}
