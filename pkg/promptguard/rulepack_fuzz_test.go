package promptguard

import "testing"

func FuzzRulepackLoad(f *testing.F) {
	f.Add([]byte("version: 3\nkind: rules\nrules: []\n"))
	f.Add([]byte(testV3Policy))
	f.Fuzz(func(_ *testing.T, data []byte) {
		//nolint:errcheck,gosec // fuzz는 panic만 찾으므로 임의 입력의 decode 실패는 정상 결과다.
		decodeRulepackFile("fuzz.yml", data)
	})
}

func FuzzSplitTextSegments(f *testing.F) {
	f.Add("ordinary text")
	f.Add("```text\nquoted\n```\n> value")
	f.Fuzz(func(_ *testing.T, input string) {
		_, _ = buildEvaluationSegments(input)
	})
}
