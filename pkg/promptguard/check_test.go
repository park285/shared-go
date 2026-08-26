package promptguard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
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
					t.Parallel()

					guard := newDecisionGuard(decision, nil)
					evaluation, err := guard.Check(CheckRequest{Text: name, Source: source, Enforcement: mode})

					if evaluation.Decision != decision {
						t.Fatalf("Check() decision = %q, want %q", evaluation.Decision, decision)
					}

					wantRejected := enforcementRejects(mode, decision)

					blocked, rejected := errors.AsType[*BlockedError](err)
					if rejected != wantRejected {
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
			t.Parallel()

			var (
				detected atomic.Int32
				observed atomic.Int32
			)

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

	for _, guard := range []*Guard{nil, {cfg: Config{Enabled: false}}} {
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
