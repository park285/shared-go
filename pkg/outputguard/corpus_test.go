package outputguard

import (
	"bufio"
	"os"
	"strings"
	"testing"

	sharedjson "github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type outputCorpusCase struct {
	ID               string       `json:"id"`
	Text             string       `json:"text"`
	TextRepeat       string       `json:"text_repeat"`
	TextRepeatCount  int          `json:"text_repeat_count"`
	ProtectedTexts   []string     `json:"protected_texts"`
	ExpectedDecision Decision     `json:"expected_decision"`
	ExpectedReasons  []ReasonCode `json:"expected_reasons"`
}

func TestOutputGuardCorpus(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/corpus-v2.jsonl")
	require.NoError(t, err)
	defer file.Close()

	guard := NewGuard()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var test outputCorpusCase
		require.NoError(t, sharedjson.Unmarshal(scanner.Bytes(), &test))
		t.Run(test.ID, func(t *testing.T) {
			text := test.Text
			if test.TextRepeatCount > 0 {
				text = strings.Repeat(test.TextRepeat, test.TextRepeatCount)
			}
			var evaluation Evaluation
			if len(test.ProtectedTexts) > 0 {
				bound, bindErr := guard.Bind(test.ProtectedTexts)
				require.NoError(t, bindErr)
				evaluation = bound.Check(text)
			} else {
				evaluation = guard.Check(CheckRequest{Text: text})
			}
			assert.Equal(t, test.ExpectedDecision, evaluation.Decision)
			assert.Equal(t, len(test.ExpectedReasons), len(evaluation.ReasonCodes))
			if len(test.ExpectedReasons) > 0 {
				assert.Equal(t, test.ExpectedReasons, evaluation.ReasonCodes)
			}
		})
	}
	require.NoError(t, scanner.Err())
}
