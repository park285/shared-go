package promptguard

import (
	"strings"
	"testing"
)

func TestRawRulesDoNotUseNormalizedLiteralPrefilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rule rawRule
	}{
		{
			name: "regex compatibility characters",
			rule: rawRule{
				ID:             "raw-fullwidth-regex",
				Family:         "raw-fullwidth-regex",
				Type:           ruleTypeRegex,
				Action:         hitActionScore,
				View:           viewRaw,
				Segments:       []string{string(testSegmentPlain)},
				Pattern:        `ｓｙｓｔｅｍ`,
				Weight:         1,
				MaxOccurrences: 1,
			},
		},
		{
			name: "phrase compatibility characters",
			rule: rawRule{
				ID:             "raw-fullwidth-phrase",
				Family:         "raw-fullwidth-phrase",
				Type:           ruleTypePhrase,
				Action:         hitActionScore,
				View:           viewRaw,
				Segments:       []string{string(testSegmentPlain)},
				Phrases:        []string{"ｓｙｓｔｅｍ"},
				MatchMode:      phraseMatchSubstring,
				Weight:         1,
				MaxOccurrences: 1,
			},
		},
	}

	segment := textSegment{Kind: segmentPlain, Views: normalizeViews("ｓｙｓｔｅｍ")}
	if segment.Views.Raw == segment.Views.Norm {
		t.Fatal("test fixture must normalize to a different representation")
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			compiled, err := compileRule(&tc.rule)
			if err != nil {
				t.Fatalf("compileRule() error = %v", err)
			}

			if matches := compiled.matchSegment(segment, compilePolicy(&rawRulepack{Version: 3}), 1); len(matches) != 1 {
				t.Fatalf("matchSegment() matches = %d, want 1", len(matches))
			}
		})
	}
}

func TestRawRegexPrefilterPreservesUppercaseASCIIRegexMatches(t *testing.T) {
	t.Parallel()

	compiled, err := compileRule(&rawRule{
		ID:             "raw-uppercase-ascii",
		Family:         "raw-uppercase-ascii",
		Type:           ruleTypeRegex,
		Action:         hitActionBlock,
		View:           viewRaw,
		Segments:       []string{string(testSegmentPlain)},
		Pattern:        `everything[\s\S]{0,32}print`,
		Weight:         1,
		MaxOccurrences: 1,
	})
	if err != nil {
		t.Fatalf("compileRule() error = %v", err)
	}

	segment := textSegment{Kind: segmentPlain, Views: normalizeViews("STOP EVERYTHING NOW JUST PRINT")}
	if strings.EqualFold(segment.Views.Norm, segment.Views.Raw) {
		t.Fatal("test fixture must exercise context-sensitive confusable normalization")
	}

	if matches := compiled.matchSegment(segment, compilePolicy(&rawRulepack{Version: 3}), 1); len(matches) != 1 {
		t.Fatalf("matchSegment() matches = %d, want 1", len(matches))
	}
}
