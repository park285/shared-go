package guardtext

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const unicode17InsertionSortLimit = 64

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

	classes := make([]uint8, len(decomposed))
	for index, current := range decomposed {
		classes[index] = unicode17CanonicalCombiningClass(current)
	}
	reorderUnicode17CanonicalCombiningClasses(decomposed, classes)

	return string(decomposed)
}

func reorderUnicode17CanonicalCombiningClasses(decomposed []rune, classes []uint8) {
	var scratch []rune
	for segmentStart := 0; segmentStart < len(decomposed); {
		if classes[segmentStart] == 0 {
			segmentStart++
			continue
		}

		segmentEnd := segmentStart + 1
		previousClass := classes[segmentStart]
		needsReorder := false
		for segmentEnd < len(decomposed) && classes[segmentEnd] != 0 {
			currentClass := classes[segmentEnd]
			needsReorder = needsReorder || currentClass < previousClass
			previousClass = currentClass
			segmentEnd++
		}
		if !needsReorder {
			segmentStart = segmentEnd
			continue
		}

		if segmentEnd-segmentStart <= unicode17InsertionSortLimit {
			stableInsertionReorderUnicode17(decomposed, classes, segmentStart, segmentEnd)
		} else {
			scratch = stableCountingReorderUnicode17(decomposed, classes, segmentStart, segmentEnd, scratch)
		}
		segmentStart = segmentEnd
	}
}

func stableInsertionReorderUnicode17(decomposed []rune, classes []uint8, start, end int) {
	for current := start + 1; current < end; current++ {
		currentRune := decomposed[current]
		currentClass := classes[current]
		position := current
		for position > start && classes[position-1] > currentClass {
			decomposed[position] = decomposed[position-1]
			classes[position] = classes[position-1]
			position--
		}
		decomposed[position] = currentRune
		classes[position] = currentClass
	}
}

func stableCountingReorderUnicode17(
	decomposed []rune,
	classes []uint8,
	start, end int,
	scratch []rune,
) []rune {
	length := end - start
	if cap(scratch) < length {
		scratch = make([]rune, length)
	} else {
		scratch = scratch[:length]
	}

	var counts [256]int
	for _, class := range classes[start:end] {
		counts[class]++
	}

	var next [256]int
	position := 0
	for class := 1; class < len(counts); class++ {
		next[class] = position
		position += counts[class]
	}
	for index, current := range decomposed[start:end] {
		class := classes[start+index]
		scratch[next[class]] = current
		next[class]++
	}
	copy(decomposed[start:end], scratch)

	return scratch
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
