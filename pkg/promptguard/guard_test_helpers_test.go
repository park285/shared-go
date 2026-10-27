package promptguard

import (
	"fmt"
	"testing"
)

// compileTestPack은 로더가 rules 파일을 묶는 것과 같은 방식으로 단일 규칙 pack을 만든다.
func compileTestPack(tb testing.TB, raw *rawRulepack) compiledPack {
	tb.Helper()

	rules, err := compileRules(raw)
	if err != nil {
		tb.Fatalf("compileRules() error = %v", err)
	}

	return compiledPack{Version: 3, Kind: rulepackKindRules, Policy: compilePolicy(raw), Rules: rules}
}

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

func splitTextSegments(text string) []textSegment {
	var segments []textSegment

	walkTextSegments(text, func(segment textSegment) {
		segments = append(segments, segment)
	})

	return segments
}

func buildEvaluationSegmentsFiltered(text string, includeAggregate func(*aggregateTail, textSegment) bool) []textSegment {
	var raw, aggregate []textSegment

	walkEvaluationSegments(text, includeAggregate, func(segment textSegment) {
		raw = append(raw, segment)
	}, func(segment textSegment) {
		aggregate = append(aggregate, segment)
	})

	return append(raw, aggregate...)
}
