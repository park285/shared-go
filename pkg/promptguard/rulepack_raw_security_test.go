package promptguard

import "testing"

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
				Segments:       []string{string(segmentPlain)},
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
				Segments:       []string{string(segmentPlain)},
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
