package promptguard

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records = append(h.records, r.Clone())

	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func (h *captureHandler) attr(record slog.Record, key string) (slog.Value, bool) {
	var (
		value slog.Value
		found bool
	)

	record.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			value = a.Value
			found = true

			return false
		}

		return true
	})

	return value, found
}

func TestFallbackEvaluationBlocksAndLogsFixedReason(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := &Guard{
		cfg:    Config{Enabled: true},
		logger: slog.New(handler),
	}

	policy := compiledPolicy{BlockThreshold: 1.0, ReviewThreshold: 0.55}
	evaluation := guard.fallbackEvaluation(policy, SourceUserPrompt, fallbackCauseDetectorError)

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("fallbackEvaluation() decision = %q, want %q", evaluation.Decision, DecisionBlock)
	}

	if !evaluation.FallbackBlocked {
		t.Fatal("fallbackEvaluation() FallbackBlocked = false, want true")
	}

	if evaluation.Source != SourceUserPrompt {
		t.Fatalf("fallbackEvaluation() source = %q, want %q", evaluation.Source, SourceUserPrompt)
	}

	if evaluation.Threshold != policy.BlockThreshold || evaluation.ReviewThreshold != policy.ReviewThreshold {
		t.Fatalf("fallbackEvaluation() thresholds = (%v, %v), want (%v, %v)",
			evaluation.Threshold, evaluation.ReviewThreshold, policy.BlockThreshold, policy.ReviewThreshold)
	}

	if len(handler.records) != 1 {
		t.Fatalf("fallbackEvaluation() emitted %d log records, want 1", len(handler.records))
	}

	record := handler.records[0]
	if record.Level != slog.LevelError {
		t.Fatalf("fallbackEvaluation() log level = %v, want Error", record.Level)
	}

	reasonValue, ok := handler.attr(record, "reason")
	if !ok {
		t.Fatal("fallbackEvaluation() log missing reason attribute")
	}

	if reasonValue.String() != ruleEvaluationFallback {
		t.Fatalf("fallbackEvaluation() reason = %q, want %q", reasonValue.String(), ruleEvaluationFallback)
	}

	sourceValue, ok := handler.attr(record, "source")
	if !ok || sourceValue.String() != string(SourceUserPrompt) {
		t.Fatalf("fallbackEvaluation() log source = %q (found=%v), want %q", sourceValue.String(), ok, SourceUserPrompt)
	}

	causeValue, ok := handler.attr(record, "cause")
	if !ok || causeValue.String() != string(fallbackCauseDetectorError) {
		t.Fatalf("fallbackEvaluation() log cause = %q (found=%v), want %q", causeValue.String(), ok, fallbackCauseDetectorError)
	}
}

func TestEvaluateFallbackLogsClassifiedCauseWithoutDetectorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		noCache   bool
		detect    func(string) (Evaluation, error)
		wantCause string
	}{
		{
			name: "detector error",
			detect: func(string) (Evaluation, error) {
				return Evaluation{}, errors.New("SENSITIVE_DETECTOR_ERROR")
			},
			wantCause: "detector_error",
		},
		{
			name: "invalid detector decision",
			detect: func(string) (Evaluation, error) {
				return Evaluation{Decision: "SENSITIVE_DECISION"}, nil
			},
			wantCause: "invalid_detector_decision",
		},
		{
			name:    "cache unavailable",
			noCache: true,
			detect: func(string) (Evaluation, error) {
				return evaluationForDecision(DecisionAllow), nil
			},
			wantCause: "cache_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &captureHandler{}

			guard := newDecisionGuard(DecisionAllow, nil)

			guard.logger = slog.New(handler)
			guard.evaluateInputFn = tt.detect

			if tt.noCache {
				guard.cache = nil
			}

			evaluation, err := guard.Check(CheckRequest{Text: "input", Source: SourceUserPrompt, Enforcement: EnforcementInteractive})
			if _, ok := errors.AsType[*BlockedError](err); !ok || !evaluation.FallbackBlocked {
				t.Fatalf("Check() = (%#v, %v), want fallback block", evaluation, err)
			}

			assertSingleFallbackLog(t, handler, tt.wantCause)
		})
	}
}

func assertSingleFallbackLog(t *testing.T, handler *captureHandler, wantCause string) {
	t.Helper()

	if len(handler.records) != 1 {
		t.Fatalf("emitted %d log records, want 1", len(handler.records))
	}

	record := handler.records[0]

	causeValue, ok := handler.attr(record, "cause")
	if !ok || causeValue.String() != wantCause {
		t.Fatalf("log cause = %q (found=%v), want %q", causeValue.String(), ok, wantCause)
	}

	reasonValue, _ := handler.attr(record, "reason")
	if reasonValue.String() != ruleEvaluationFallback {
		t.Fatalf("log reason = %q, want %q", reasonValue.String(), ruleEvaluationFallback)
	}

	leaked := strings.Contains(record.Message, "SENSITIVE")

	record.Attrs(func(a slog.Attr) bool {
		leaked = leaked || strings.Contains(a.Value.String(), "SENSITIVE")

		return !leaked
	})

	if leaked {
		t.Fatal("fallback log leaked detector text")
	}
}
