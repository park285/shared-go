package promptguard

import "testing"

func evaluateForTest(t testing.TB, guard *Guard, input string) Evaluation {
	t.Helper()

	evaluation, err := guard.Check(CheckRequest{
		Text:        input,
		Source:      SourceUserPrompt,
		Enforcement: EnforcementObserve,
	})
	if err != nil {
		t.Fatalf("Check(observe) error = %v", err)
	}

	return evaluation
}

func checkInteractiveForTest(t testing.TB, guard *Guard, input string) error {
	t.Helper()

	_, err := guard.Check(CheckRequest{
		Text:        input,
		Source:      SourceUserPrompt,
		Enforcement: EnforcementInteractive,
	})

	return err
}
