package promptguard

import (
	"bufio"
	jsonv2 "encoding/json/v2"
	"os"
	"slices"
	"testing"
)

type corpusCase struct {
	ID               string   `json:"id"`
	Class            string   `json:"class"`
	Locale           string   `json:"locale"`
	Surface          string   `json:"surface"`
	Input            string   `json:"input"`
	ExpectedDecision Decision `json:"expected_decision"`
	ExpectedRules    []string `json:"expected_rules"`
}

func TestEmbeddedRulepackCorpusV3(t *testing.T) {
	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, nil)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}

	cases := readCorpusCases(t, "testdata/corpus-v3.jsonl")
	assertCorpusMinimums(t, cases)

	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			evaluation := evaluateForTest(t, guard, tc.Input)
			if evaluation.Decision != tc.ExpectedDecision {
				t.Fatalf("detected decision = %q, want %q (hits=%v norm=%q)", evaluation.Decision, tc.ExpectedDecision, matchedRuleIDs(evaluation.Hits), normalizeViews(tc.Input).Joined)
			}

			actualRules := matchedRuleIDs(evaluation.Hits)

			for _, expected := range tc.ExpectedRules {
				if !slices.Contains(actualRules, expected) {
					t.Errorf("detected rules = %v, want %q", actualRules, expected)
				}
			}
		})
	}
}

func readCorpusCases(t *testing.T, path string) []corpusCase {
	t.Helper()

	file, err := os.Open(path) //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 읽는다.
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer file.Close()

	var cases []corpusCase

	scanner := bufio.NewScanner(file)

	for line := 1; scanner.Scan(); line++ {
		var tc corpusCase

		if err := jsonv2.Unmarshal(scanner.Bytes(), &tc); err != nil {
			t.Fatalf("decode corpus line %d: %v", line, err)
		}

		if tc.ID == "" || tc.Input == "" || tc.ExpectedDecision == "" {
			t.Fatalf("corpus line %d has an incomplete contract", line)
		}

		cases = append(cases, tc)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}

	return cases
}

func assertCorpusMinimums(t *testing.T, cases []corpusCase) {
	t.Helper()

	counts := map[string]int{}
	webMalicious := 0

	for _, tc := range cases {
		counts[tc.Class]++
		if tc.Class == "malicious" && tc.Surface == "web_search_result" {
			webMalicious++
		}
	}

	minimums := map[string]int{"malicious": 20, "transformation": 12, "benign": 20, "ambiguous": 12}
	for class, minimum := range minimums {
		if counts[class] < minimum {
			t.Errorf("%s corpus count = %d, want >= %d", class, counts[class], minimum)
		}
	}

	if webMalicious < 8 {
		t.Errorf("malicious web corpus count = %d, want >= 8", webMalicious)
	}
}
