package promptguard

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundaryAggregatesRollOnlyAcrossExplicitParts(t *testing.T) {
	t.Parallel()

	segments := []textSegment{
		{Kind: segmentPlain, Part: 0, Views: normalizeViews("first")},
		{Kind: segmentQuote, Part: 0, Views: normalizeViews("middle")},
		{Kind: segmentCode, Part: 0, Views: normalizeViews("last")},
	}
	adjacent := buildBoundaryAggregates(segments, false)
	if len(adjacent) != 2 || strings.Contains(adjacent[1].Views.Raw, "first") {
		t.Fatalf("adjacent aggregates = %#v, want only the immediate left segment", adjacent)
	}

	for i := range segments {
		segments[i].Part = i
	}
	rolling := buildBoundaryAggregates(segments, true)
	if len(rolling) != 2 || !strings.Contains(rolling[1].Views.Raw, "firstmiddlelast") {
		t.Fatalf("rolling aggregates = %#v, want explicit multi-part context", rolling)
	}
}

func TestRuneWindowHelpersPreserveUTF8Boundaries(t *testing.T) {
	t.Parallel()

	const input = "가😀나다"
	if got := firstRunes(input, 2); got != "가😀" || !utf8.ValidString(got) {
		t.Fatalf("firstRunes() = %q, want valid two-rune prefix", got)
	}
	if got := lastRunes(input, 2); got != "나다" || !utf8.ValidString(got) {
		t.Fatalf("lastRunes() = %q, want valid two-rune suffix", got)
	}

	combined, trimmed := appendAggregateTailView(strings.Repeat("가", boundaryWindowRunes), "", "😀")
	if trimmed != 1 || utf8.RuneCountInString(combined) != boundaryWindowRunes || !utf8.ValidString(combined) {
		t.Fatalf("appendAggregateTailView() = (%d runes, trimmed=%d, valid=%v)", utf8.RuneCountInString(combined), trimmed, utf8.ValidString(combined))
	}
}
