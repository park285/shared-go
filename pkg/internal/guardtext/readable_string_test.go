package guardtext

import (
	"strings"
	"testing"
)

func readableStringCases() []string {
	return []string{
		"",
		"a",
		"readable ascii text",
		"한글 텍스트 입니다",
		"\x00\x01\x02\x03",
		"mostly readable\x00",
		"\xff\xfe",
		"\xc3",
		"\xed\xa0\x80",
		"�",
		"valid � replacement",
		"tab\tnewline\n",
		strings.Repeat("x", 300),
		strings.Repeat("\x01", 300),
		strings.Repeat("가", 100),
		"emoji 🙂 payload",
		"\xf0\x9f\x99",
	}
}

func TestIsReadableStringMatchesIsReadableText(t *testing.T) {
	t.Parallel()

	for _, candidate := range readableStringCases() {
		want := IsReadableText([]byte(candidate))
		if got := IsReadableString(candidate); got != want {
			t.Fatalf("IsReadableString(%q) = %v, want %v (IsReadableText)", candidate, got, want)
		}
	}
}

func FuzzIsReadableStringMatchesIsReadableText(f *testing.F) {
	for _, candidate := range readableStringCases() {
		f.Add(candidate)
	}

	f.Fuzz(func(t *testing.T, candidate string) {
		if IsReadableString(candidate) != IsReadableText([]byte(candidate)) {
			t.Fatalf("divergence for %q", candidate)
		}
	})
}

func TestIsReadableStringAllocatesNothing(t *testing.T) {
	candidate := strings.Repeat("readable payload ", 64)

	if allocs := testing.AllocsPerRun(50, func() { _ = IsReadableString(candidate) }); allocs != 0 {
		t.Fatalf("IsReadableString allocations = %.0f, want 0", allocs)
	}
}
