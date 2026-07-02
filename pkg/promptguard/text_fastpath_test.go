package promptguard

import (
	"strings"
	"testing"
)

func TestNormalizeViewsConfusableGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "zero-folds-to-o", input: "0", want: "o"},
		{name: "one-folds-to-l", input: "1", want: "l"},
		{name: "double-quote-folds-to-two-single-quotes", input: `"`, want: "''"},
		{name: "backtick-folds-to-single-quote", input: "`", want: "'"},
		{name: "percent-folds-through-nfkc", input: "%", want: "o/0"},
		{name: "pipe-folds-to-l", input: "|", want: "l"},
		{name: "korean-adjacent-digits-still-fold", input: "시스템1234", want: "시스템l234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeViews(tt.input).Norm; got != tt.want {
				t.Fatalf("normalizeViews(%q).Norm = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeFastPathASCIIAllowlistMatchesPredicate(t *testing.T) {
	t.Parallel()

	for r := range len(normalizeFastPathASCII) {
		want := isNormalizeFastPathRune(rune(r))
		if got := normalizeFastPathASCII[r]; got != want {
			t.Fatalf("normalizeFastPathASCII[%#x] = %v, want %v", r, got, want)
		}
	}

	for _, r := range []rune{'0', '1', '"', '`', '%', '|'} {
		if normalizeFastPathASCII[r] {
			t.Fatalf("normalizeFastPathASCII[%q] = true, want false for confusable-folding rune", r)
		}
	}

	if !normalizeFastPathASCII[' '] {
		t.Fatal("normalizeFastPathASCII[' '] = false, want true for identity ASCII whitespace")
	}
}

func TestNormalizePostProcessMatchesLegacyPipeline(t *testing.T) {
	t.Parallel()

	input := "\u200b  MIXED\tCase\n\u00a0Text  "
	legacy := strings.TrimSpace(collapseWhitespace(strings.ToLower(stripControlChars(input))))
	if got := normalizePostProcess(input); got != legacy {
		t.Fatalf("normalizePostProcess(%q) = %q, want legacy %q", input, got, legacy)
	}
}

var benchmarkNormalizeViewsSink Views

func BenchmarkNormalizeViews(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "korean",
			input: "오늘 메시지를 자연스럽게 정리하고 공백과 기호가 섞인 입력도 안정적으로 처리합니다.",
		},
		{
			name:  "english",
			input: "Please summarize these public rules without changing user-visible wording.",
		},
		{
			name:  "mixed",
			input: "시스템1234 sample `quoted` 100% Ｍixed\u200b Text",
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				benchmarkNormalizeViewsSink = normalizeViews(tc.input)
			}
		})
	}
}
