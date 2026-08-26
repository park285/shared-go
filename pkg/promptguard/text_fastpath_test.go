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

func TestNormalizePostProcessMatchesLegacyPipeline(t *testing.T) {
	t.Parallel()

	input := "\u200b  MIXED\tCase\n\u00a0Text  "
	baseline := strings.TrimSpace(collapseWhitespace(strings.ToLower(stripControlChars(input))))

	if got := normalizePostProcess(input); got != baseline {
		t.Fatalf("normalizePostProcess(%q) = %q, want baseline %q", input, got, baseline)
	}
}

var benchmarkNormalizeViewsSink Views

func benchmarkNormalizeViews(b *testing.B, input string) {
	b.Helper()

	b.ReportAllocs()

	for b.Loop() {
		benchmarkNormalizeViewsSink = normalizeViews(input)
	}
}

func BenchmarkNormalizeViewsKorean(b *testing.B) {
	benchmarkNormalizeViews(b, "오늘 메시지를 자연스럽게 정리하고 공백과 기호가 섞인 입력도 안정적으로 처리합니다.")
}

func BenchmarkNormalizeViewsEnglish(b *testing.B) {
	benchmarkNormalizeViews(b, "Please summarize these public rules without changing user-visible wording.")
}

func BenchmarkNormalizeViewsMixed(b *testing.B) {
	benchmarkNormalizeViews(b, "시스템1234 sample `quoted` 100% Ｍixed\u200b Text")
}
