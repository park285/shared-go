package stringutil

import (
	"strings"
	"testing"
)

func BenchmarkTruncateString(b *testing.B) {
	for _, benchmark := range []struct {
		name     string
		input    string
		maxRunes int
	}{
		{name: "ascii_no_truncation", input: strings.Repeat("a", 16*1024), maxRunes: 32 * 1024},
		{name: "utf8_no_truncation", input: strings.Repeat("가", 16*1024), maxRunes: 32 * 1024},
		{name: "ascii_truncated", input: strings.Repeat("a", 16*1024), maxRunes: 1024},
		{name: "utf8_truncated", input: strings.Repeat("가", 16*1024), maxRunes: 1024},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				truncateStringSink = TruncateString(benchmark.input, benchmark.maxRunes)
			}
		})
	}
}
