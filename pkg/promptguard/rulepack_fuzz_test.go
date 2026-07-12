package promptguard

import "testing"

func FuzzRulepackLoad(f *testing.F) {
	f.Add([]byte("version: 3\nkind: rules\nrules: []\n"))
	f.Add([]byte(testV3Policy))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeRulepackFile("fuzz.yml", data)
	})
}

func FuzzSplitTextSegments(f *testing.F) {
	f.Add("ordinary text")
	f.Add("```text\nquoted\n```\n> value")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = buildEvaluationSegments(input)
	})
}
