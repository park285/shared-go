package promptguard

import (
	"fmt"
	"testing"
)

func evaluateForTest(tb testing.TB, guard *Guard, input string) Evaluation {
	tb.Helper()

	evaluation, err := guard.Check(CheckRequest{
		Text:        input,
		Source:      SourceUserPrompt,
		Enforcement: EnforcementObserve,
	})
	if err != nil {
		tb.Fatalf("Check(observe) error = %v", err)
	}

	return evaluation
}

func checkInteractiveForTest(tb testing.TB, guard *Guard, input string) error {
	tb.Helper()

	if _, err := guard.Check(CheckRequest{
		Text:        input,
		Source:      SourceUserPrompt,
		Enforcement: EnforcementInteractive,
	}); err != nil {
		return fmt.Errorf("check: %w", err)
	}

	return nil
}
