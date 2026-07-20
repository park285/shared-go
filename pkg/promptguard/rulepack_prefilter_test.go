package promptguard

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRequiredLiteralBranchesPreserveAlternationConjunctions(t *testing.T) {
	pattern := regexp.MustCompile(`(?i)(?:show.{0,3}prompt|reveal.{0,3}instructions)`)
	branches := requiredRegexLiteralBranches(pattern)
	if len(branches) != 2 {
		t.Fatalf("branches = %#v, want two alternatives", branches)
	}
	if containsAnyLiteralBranch("previous instructions", branches) {
		t.Fatal("branch prefilter accepted input missing the branch action")
	}
	for _, input := range []string{"show prompt", "reveal instructions"} {
		if !containsAnyLiteralBranch(input, branches) {
			t.Fatalf("branch prefilter rejected regex candidate %q: %#v", input, branches)
		}
	}
}

func TestRequiredLiteralBranchesAdmitEmbeddedCorpusRegexMatches(t *testing.T) {
	guard := newTestGuardFromRulepacks(t)
	for _, tc := range readCorpusCases(t, "testdata/corpus-v3.jsonl") {
		segments, exceeded := buildEvaluationSegmentsFiltered(tc.Input, guard.aggregateMayMatch)
		if exceeded {
			continue
		}
		decoded, _ := guard.decodedTextSegments(tc.Input)
		segments = append(segments, decoded...)
		for _, pack := range guard.packs {
			for ruleIndex := range pack.Rules {
				rule := &pack.Rules[ruleIndex]
				if rule.Type != ruleTypeRegex || rule.View == viewRaw || len(rule.RequiredLiteralBranches) == 0 {
					continue
				}
				for _, segment := range segments {
					text := segmentView(segment, rule.View)
					if rule.Pattern.MatchString(text) && !containsAnyLiteralBranch(text, rule.RequiredLiteralBranches) {
						t.Fatalf("case %q rule %q matched regex but failed branch prefilter for %q", tc.ID, rule.ID, text)
					}
				}
			}
		}
	}
}

func TestEmbeddedRulesHaveRequiredLiteralPrefilters(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	for _, pack := range guard.packs {
		for _, rule := range pack.Rules {
			if len(rule.RequiredLiteralGroups) == 0 {
				t.Errorf("rule %q has no required literal prefilter", rule.ID)
			}
		}
	}
}

func TestAggregatePrefilterFailsOpenAtNodeLimit(t *testing.T) {
	t.Parallel()

	literals := make([]string, maxAggregateAutomatonNodes)
	for index := range literals {
		literals[index] = string(rune(0x1000+index)) + "-unique-literal"
	}
	filter := newAggregateViewPrefilter(literals, false)
	if !filter.hasRules || !filter.unfiltered {
		t.Fatalf("aggregate filter = %#v, want bounded fail-open filter", filter)
	}
}

func TestAggregateAutomatonMatchesReference(t *testing.T) {
	t.Parallel()

	literals := []string{"he", "she", "hers", "정책"}
	automaton, complete := newAggregateLiteralAutomaton(literals)
	if !complete {
		t.Fatal("automaton unexpectedly exceeded its node budget")
	}
	for _, input := range []string{"", "ushers", "her", "she", "정책 우회", "보안 규칙"} {
		want := false
		for _, literal := range literals {
			want = want || strings.Contains(input, literal)
		}
		if got := automaton.matches([]byte(input)); got != want {
			t.Errorf("matches(%q) = %t, want %t", input, got, want)
		}
		if got := automaton.matchesString(input); got != want {
			t.Errorf("matchesString(%q) = %t, want %t", input, got, want)
		}
	}
}

func TestAggregateAutomatonNormalizedASCIIMatchesNormalize(t *testing.T) {
	t.Parallel()

	for _, literal := range []string{"a", "a b", "ab", "o", "system", "</"} {
		automaton, complete := newAggregateLiteralAutomaton([]string{literal})
		if !complete {
			t.Fatalf("automaton for %q exceeded its node budget", literal)
		}
		for first := range utf8.RuneSelf {
			for second := range utf8.RuneSelf {
				input := []byte{byte(first), byte(second)}
				got, ascii := automaton.matchesNormalizedASCII(input)
				if !ascii {
					t.Fatalf("matchesNormalizedASCII(%q) reported non-ASCII", input)
				}
				want := automaton.matchesString(normalizeText(string(input)))
				if got != want {
					t.Fatalf("matchesNormalizedASCII(%q, %q) = %t, want %t", literal, input, got, want)
				}
			}
		}
		for _, input := range []string{"a  b", " a b ", "a\tb", "A B", "a\x00b"} {
			got, ascii := automaton.matchesNormalizedASCII([]byte(input))
			if !ascii {
				t.Fatalf("matchesNormalizedASCII(%q) reported non-ASCII", input)
			}
			want := automaton.matchesString(normalizeText(input))
			if got != want {
				t.Fatalf("matchesNormalizedASCII(%q, %q) = %t, want %t", literal, input, got, want)
			}
		}
	}
}

func TestAggregateAutomatonNormalizedASCIIRejectsNonASCII(t *testing.T) {
	t.Parallel()

	automaton, complete := newAggregateLiteralAutomaton([]string{"system"})
	if !complete {
		t.Fatal("automaton unexpectedly exceeded its node budget")
	}
	if matched, ascii := automaton.matchesNormalizedASCII([]byte{0xff}); matched || ascii {
		t.Fatalf("matchesNormalizedASCII(non-ASCII) = (%t, %t), want (false, false)", matched, ascii)
	}
}

func TestAggregatePrefilterCoversEveryEmbeddedRuleLiteral(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	for _, pack := range guard.packs {
		for _, rule := range pack.Rules {
			var filter aggregateViewPrefilter
			var candidateView func(string) string
			switch rule.View {
			case viewRaw:
				filter = guard.aggregateFilter.rawNorm
				candidateView = normalizeText
			case viewAggregateNorm:
				filter = guard.aggregateFilter.norm
				candidateView = func(value string) string { return normalizeViews(value).Norm }
			case viewAggregateJoined:
				filter = guard.aggregateFilter.joined
				candidateView = func(value string) string { return normalizeViews(value).Joined }
			default:
				continue
			}
			if !filter.hasRules || filter.unfiltered {
				t.Fatalf("rule %q filter = %#v, want a complete literal filter", rule.ID, filter)
			}
			for _, literal := range rule.AggregatePrefilter {
				candidate := candidateView("ordinary " + literal + " context")
				if !filter.automaton.matchesString(candidate) {
					t.Errorf("rule %q literal %q is absent from the aggregate prefilter", rule.ID, literal)
				}
			}
		}
	}
}

func TestAggregatePrefilterKeepsRawPunctuationSplit(t *testing.T) {
	t.Parallel()

	rule, err := compileRule(&rawRule{
		ID:             "raw-punctuation-split",
		Family:         "raw-punctuation-split",
		Type:           ruleTypePhrase,
		Action:         hitActionBlock,
		View:           viewRaw,
		Segments:       []string{string(segmentPlain)},
		Phrases:        []string{"zxq!!!!!jkv"},
		MatchMode:      phraseMatchSubstring,
		Weight:         1,
		MaxOccurrences: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	filter := compileAggregatePrefilters([]compiledPack{{Rules: []compiledRule{rule}}})
	segments, exceeded := buildEvaluationSegmentsFiltered(
		JoinParts("zxq!!", "!!!jkv"),
		filter.mayMatch,
	)
	if exceeded {
		t.Fatal("segment budget unexpectedly exceeded")
	}
	policy := compilePolicy(&rawRulepack{Version: 3})
	for _, segment := range segments {
		if segment.Aggregate && len(rule.matchSegment(segment, policy, 1)) == 1 {
			return
		}
	}
	t.Fatal("raw punctuation split was removed by aggregate prefilter")
}

func TestRollingBenchmarkFixtureExercisesAggregatePath(t *testing.T) {
	t.Parallel()

	guard := newTestGuardFromRulepacks(t)
	segments, exceeded := buildEvaluationSegmentsFiltered(rollingAggregateBenchmarkInput(), guard.aggregateMayMatch)
	if exceeded {
		t.Fatal("segment budget unexpectedly exceeded")
	}
	for _, segment := range segments {
		if segment.Aggregate {
			return
		}
	}
	t.Fatal("rolling benchmark fixture did not exercise an aggregate segment")
}
