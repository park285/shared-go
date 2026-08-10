package promptguard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckEnforcementMatrixForEverySource(t *testing.T) {
	t.Parallel()

	sources := []Source{
		SourceUserPrompt,
		SourcePromptBundle,
		SourceRetrievedMemory,
		SourceMemoryCandidate,
		SourceSessionPatch,
		SourceSimulationState,
		SourceLawContext,
		SourceSessionContext,
		SourceChatLog,
		SourceWebSearchResult,
		SourceImagePrompt,
	}
	modes := []Enforcement{EnforcementObserve, EnforcementInteractive, EnforcementPersistent}
	decisions := []Decision{DecisionAllow, DecisionReview, DecisionBlock}

	for _, source := range sources {
		for _, mode := range modes {
			for _, decision := range decisions {
				name := fmt.Sprintf("%s/%d/%s", source, mode, decision)
				t.Run(name, func(t *testing.T) {
					guard := newDecisionGuard(decision, nil)
					evaluation, err := guard.Check(CheckRequest{Text: name, Source: source, Enforcement: mode})

					if evaluation.Decision != decision {
						t.Fatalf("Check() decision = %q, want %q", evaluation.Decision, decision)
					}
					wantRejected := enforcementRejects(mode, decision)
					var blocked *BlockedError
					if errors.As(err, &blocked) != wantRejected {
						t.Fatalf("Check() error = %v, rejected=%v", err, wantRejected)
					}
					if blocked != nil && blocked.Decision != decision {
						t.Fatalf("BlockedError.Decision = %q, want %q", blocked.Decision, decision)
					}
				})
			}
		}
	}
}

func TestCheckEmitsDecodeIncompleteRuleOnMissAndCacheHit(t *testing.T) {
	t.Parallel()
	payload := "ordinary safe text"
	input := base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte(url.PathEscape(payload)))))
	var events []EvaluationEvent
	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true, OnEvaluation: func(event EvaluationEvent) { events = append(events, event) }}, nil)
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}

	first, firstErr := guard.Check(CheckRequest{Text: input, Source: SourceUserPrompt, Enforcement: EnforcementObserve})
	second, secondErr := guard.Check(CheckRequest{Text: input, Source: SourcePromptBundle, Enforcement: EnforcementObserve})
	if firstErr != nil || secondErr != nil {
		t.Fatalf("observe errors = (%v, %v)", firstErr, secondErr)
	}
	if !first.DecodeIncomplete || !second.DecodeIncomplete || first.Decision != DecisionReview || second.Decision != DecisionReview {
		t.Fatalf("evaluations = (%#v, %#v)", first, second)
	}
	if len(first.DecodeLimits) == 0 || len(second.DecodeLimits) == 0 {
		t.Fatalf("decode limits = (%v, %v), want non-empty", first.DecodeLimits, second.DecodeLimits)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for i, wantSource := range []Source{SourceUserPrompt, SourcePromptBundle} {
		if events[i].Source != wantSource || events[i].CacheHit != (i == 1) || !events[i].DecodeIncomplete ||
			!slices.Contains(events[i].RuleIDs, ruleDecodeIncomplete) ||
			!slices.Contains(events[i].RuleIDs, ruleDecodeIncomplete+":"+first.DecodeLimits[0]) {
			t.Fatalf("event[%d] = %#v", i, events[i])
		}
	}
}

func TestCheckRejectsInvalidRequestBeforeEvaluation(t *testing.T) {
	t.Parallel()

	tests := []CheckRequest{
		{Text: "omitted enforcement", Source: SourceUserPrompt},
		{Text: "omitted source", Enforcement: EnforcementInteractive},
		{Text: "unknown source", Source: Source("unknown"), Enforcement: EnforcementInteractive},
		{Text: "unknown enforcement", Source: SourceUserPrompt, Enforcement: Enforcement(255)},
	}

	for _, req := range tests {
		t.Run(req.Text, func(t *testing.T) {
			var detected atomic.Int32
			var observed atomic.Int32
			guard := newDecisionGuard(DecisionAllow, func(EvaluationEvent) {
				observed.Add(1)
			})
			guard.evaluateInputFn = func(string) (Evaluation, error) {
				detected.Add(1)

				return evaluationForDecision(DecisionAllow), nil
			}

			evaluation, err := guard.Check(req)
			if !errors.Is(err, ErrInvalidCheckRequest) {
				t.Fatalf("Check() error = %v, want ErrInvalidCheckRequest", err)
			}
			if !reflect.DeepEqual(evaluation, Evaluation{}) {
				t.Fatalf("Check() evaluation = %#v, want zero", evaluation)
			}
			if detected.Load() != 0 || observed.Load() != 0 || guard.cache.Len() != 0 {
				t.Fatalf("invalid request touched detector/callback/cache: detected=%d observed=%d cache=%d", detected.Load(), observed.Load(), guard.cache.Len())
			}
		})
	}
}

func TestCheckFailsWhenGuardUnavailable(t *testing.T) {
	t.Parallel()

	for _, guard := range []*Guard{nil, &Guard{cfg: Config{Enabled: false}}} {
		evaluation, err := guard.Check(CheckRequest{
			Text:        "ordinary input",
			Source:      SourceUserPrompt,
			Enforcement: EnforcementInteractive,
		})
		if !errors.Is(err, ErrGuardUnavailable) {
			t.Fatalf("Check() error = %v, want ErrGuardUnavailable", err)
		}
		if !reflect.DeepEqual(evaluation, Evaluation{}) {
			t.Fatalf("Check() evaluation = %#v, want zero", evaluation)
		}
	}
}

func TestCheckEmitsEquivalentEventsOnMissAndHit(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		events []EvaluationEvent
	)
	guard := newDecisionGuard(DecisionReview, func(event EvaluationEvent) {
		mu.Lock()
		defer mu.Unlock()

		captured := event
		captured.RuleIDs = slices.Clone(event.RuleIDs)
		captured.Families = slices.Clone(event.Families)
		events = append(events, captured)
		if len(event.RuleIDs) > 0 {
			event.RuleIDs[0] = "mutated"
		}
		if len(event.Families) > 0 {
			event.Families[0] = "mutated"
		}
	})
	first, err := guard.Check(CheckRequest{Text: "same", Source: SourceUserPrompt, Enforcement: EnforcementInteractive})
	if err != nil {
		t.Fatalf("first Check() error = %v", err)
	}
	first.Hits[0].ID = "caller-mutated"

	second, err := guard.Check(CheckRequest{Text: "same", Source: SourceUserPrompt, Enforcement: EnforcementInteractive})
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if second.Hits[0].ID != "review_rule" {
		t.Fatalf("cached hit ID = %q, want review_rule", second.Hits[0].ID)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events=%d, want 2", len(events))
	}
	if events[0].CacheHit || !events[1].CacheHit {
		t.Fatalf("cache flags = (%v, %v), want (false, true)", events[0].CacheHit, events[1].CacheHit)
	}
	for _, event := range events {
		if event.Source != SourceUserPrompt || event.Decision != DecisionReview || event.InputBytes != len("same") {
			t.Fatalf("event = %#v", event)
		}
		if !slices.Equal(event.RuleIDs, []string{"review_rule"}) || !slices.Equal(event.Families, []string{"review_family"}) {
			t.Fatalf("event slices = rules:%v families:%v", event.RuleIDs, event.Families)
		}
	}
}

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
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !evaluation.FallbackBlocked || !slices.Contains(blocked.Rules, ruleEvaluationFallback) {
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
			_, _ = guard.Check(CheckRequest{Text: "same concurrent input", Source: SourceSessionContext, Enforcement: EnforcementPersistent})
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
