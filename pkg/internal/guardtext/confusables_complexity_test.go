package guardtext

import (
	"strconv"
	"strings"
	"testing"
)

func TestUnicode17NFDReordersLongAdversarialCombiningRun(t *testing.T) {
	t.Parallel()

	const groupSize = 4096

	input := "a" + strings.Repeat("\u1AE7", groupSize) + strings.Repeat("\u0323", groupSize)
	want := "a" + strings.Repeat("\u0323", groupSize) + strings.Repeat("\u1AE7", groupSize)

	if got := unicode17NFD(input); got != want {
		t.Fatal("unicode17NFD() did not canonically reorder long combining run")
	}
}

func TestReorderUnicode17CanonicalCombiningClassesIsStable(t *testing.T) {
	t.Parallel()

	decomposed := []rune{'a', 'b', 'c', 'd'}
	classes := []uint8{0, 230, 230, 220}
	reorderUnicode17CanonicalCombiningClasses(decomposed, classes)

	if got, want := string(decomposed), "adbc"; got != want {
		t.Fatalf("stable reorder = %q, want %q", got, want)
	}
}

func BenchmarkUnicode17NFDAdversarialMarks(b *testing.B) {
	for _, marks := range []int{4_000, 8_000, 16_000, 32_000} {
		b.Run(strconv.Itoa(marks), func(b *testing.B) {
			input := "a" + strings.Repeat("\u1AE7", marks/2) + strings.Repeat("\u0323", marks/2)
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()

			result := ""

			for b.Loop() {
				result = unicode17NFD(input)
			}

			if result == "" {
				b.Fatal("unicode17NFD returned empty output")
			}
		})
	}
}
