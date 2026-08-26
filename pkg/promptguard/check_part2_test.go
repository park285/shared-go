package promptguard

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckEmitsEventsForOversizeAndFallback(t *testing.T) {
	t.Parallel()

	var events []EvaluationEvent

	guard := newDecisionGuard(DecisionAllow, func(event EvaluationEvent) {
		events = append(events, event)
	})

	guard.maxInputBytes = 3

	if _, err := guard.Check(CheckRequest{Text: "four", Source: SourceUserPrompt, Enforcement: EnforcementInteractive}); err == nil {
		t.Fatal("oversize Check() error = nil")
	}

	if len(events) != 1 || events[0].Decision != DecisionBlock {
		t.Fatalf("oversize events = %#v", events)
	}

	events = nil
	guard.maxInputBytes = 1024
	guard.evaluateInputFn = func(string) (Evaluation, error) {
		return Evaluation{}, errors.New("SENSITIVE_DETECTOR_ERROR")
	}

	evaluation, err := guard.Check(CheckRequest{Text: "input", Source: SourceMemoryCandidate, Enforcement: EnforcementPersistent})

	blocked, ok := errors.AsType[*BlockedError](err)
	if !ok || !evaluation.FallbackBlocked || !slices.Contains(blocked.Rules, ruleEvaluationFallback) {
		t.Fatalf("fallback result = (%#v, %v)", evaluation, err)
	}

	if len(events) != 1 || events[0].Decision != DecisionBlock {
		t.Fatalf("fallback events = %#v", events)
	}
}

func TestEmbeddedCheckEventIncludesEffectivePolicyDigest(t *testing.T) {
	t.Parallel()

	var event EvaluationEvent

	guard, err := NewGuard(Config{
		Enabled:             true,
		UseEmbeddedDefaults: true,
		OnEvaluation: func(observed EvaluationEvent) {
			event = observed
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}

	_, err = guard.Check(CheckRequest{
		Text:        "ordinary input",
		Source:      SourceUserPrompt,
		Enforcement: EnforcementInteractive,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if event.PolicyDigest == "" || event.PolicyDigest != guard.PolicyDigest() {
		t.Fatalf("event.PolicyDigest = %q, guard.PolicyDigest = %q", event.PolicyDigest, guard.PolicyDigest())
	}
}

func TestReviewLogUsesBoundedSortedRuleAndFamilyFields(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := newDecisionGuard(DecisionReview, nil)

	guard.logger = slog.New(handler)
	guard.evaluateInputFn = func(string) (Evaluation, error) {
		hits := make([]Match, 0, maxLoggedMatchValues+4)
		for i := maxLoggedMatchValues + 3; i >= 0; i-- {
			hits = append(hits, Match{
				ID:     fmt.Sprintf("rule_%02d", i),
				Family: fmt.Sprintf("family_%02d", i),
				Action: hitActionScore,
				Weight: 0.6,
			})
		}

		return Evaluation{
			Decision:         DecisionReview,
			Score:            0.6,
			Hits:             hits,
			Threshold:        1,
			ReviewThreshold:  0.55,
			DistinctFamilies: len(hits),
		}, nil
	}

	if _, err := guard.Check(CheckRequest{Text: "synthetic", Source: SourceUserPrompt, Enforcement: EnforcementInteractive}); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if len(handler.records) != 1 {
		t.Fatalf("log records = %d, want 1", len(handler.records))
	}

	record := handler.records[0]
	familiesValue, ok := handler.attr(record, "families")

	if !ok {
		t.Fatal("review log missing families")
	}

	families, ok := familiesValue.Any().([]string)
	if !ok || len(families) != maxLoggedMatchValues || !slices.IsSorted(families) {
		t.Fatalf("families = %#v, want %d sorted values", familiesValue.Any(), maxLoggedMatchValues)
	}

	rulesValue, ok := handler.attr(record, "rules")
	if !ok {
		t.Fatal("review log missing rules")
	}

	rules, ok := rulesValue.Any().([]string)
	if !ok || len(rules) != maxLoggedMatchValues || !slices.IsSorted(rules) {
		t.Fatalf("rules = %#v, want %d sorted values", rulesValue.Any(), maxLoggedMatchValues)
	}

	for _, key := range []string{"families_truncated", "rules_truncated"} {
		value, found := handler.attr(record, key)
		if !found || value.Int64() != 4 {
			t.Fatalf("%s = %v (found=%v), want 4", key, value, found)
		}
	}
}

func TestCheckConcurrentSameKeyEmitsOneEventPerCaller(t *testing.T) {
	t.Parallel()

	const callers = 32

	var (
		detections atomic.Int32
		events     atomic.Int32
	)

	guard := newDecisionGuard(DecisionAllow, func(EvaluationEvent) {
		events.Add(1)
	})

	guard.evaluateInputFn = func(string) (Evaluation, error) {
		detections.Add(1)

		return evaluationForDecision(DecisionAllow), nil
	}

	start := make(chan struct{})

	var wg sync.WaitGroup

	for range callers {
		wg.Go(func() {
			<-start

			if _, err := guard.Check(CheckRequest{Text: "same concurrent input", Source: SourceSessionContext, Enforcement: EnforcementPersistent}); err != nil {
				t.Fatalf("Check() error = %v", err)
			}
		})
	}

	close(start)
	wg.Wait()

	if detections.Load() != 1 {
		t.Fatalf("detector calls = %d, want 1", detections.Load())
	}

	if events.Load() != callers {
		t.Fatalf("evaluation events = %d, want %d", events.Load(), callers)
	}
}

func newDecisionGuard(decision Decision, onEvaluation func(EvaluationEvent)) *Guard {
	return &Guard{
		cfg:           Config{Enabled: true},
		cache:         NewTTLCache[string, Evaluation](16, time.Minute),
		maxInputBytes: 1024,
		onEvaluation:  onEvaluation,
		evaluateInputFn: func(string) (Evaluation, error) {
			return evaluationForDecision(decision), nil
		},
	}
}

func evaluationForDecision(decision Decision) Evaluation {
	hit := Match{ID: "allow_rule", Family: "allow_family", Action: hitActionScore, Weight: 0.1}

	if decision == DecisionReview {
		hit = Match{ID: "review_rule", Family: "review_family", Action: hitActionScore, Weight: 0.6}
	}

	if decision == DecisionBlock {
		hit = Match{ID: "block_rule", Family: "block_family", Action: hitActionBlock, Weight: 1}
	}

	return Evaluation{
		Decision:         decision,
		Score:            hit.Weight,
		Hits:             []Match{hit},
		Threshold:        1,
		ReviewThreshold:  0.55,
		DistinctFamilies: 1,
	}
}
