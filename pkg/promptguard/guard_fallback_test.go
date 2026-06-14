package promptguard

import (
	"context"
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

func TestFallbackEvaluationReviewsAndLogsError(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := &Guard{
		cfg:    Config{Enabled: true},
		logger: slog.New(handler),
	}

	policy := compiledPolicy{BlockThreshold: 1.0, ReviewThreshold: 0.55}
	evaluation := guard.fallbackEvaluation(policy, "user_message", "singleflight failed")

	if evaluation.Decision != DecisionReview {
		t.Fatalf("fallbackEvaluation() decision = %q, want %q", evaluation.Decision, DecisionReview)
	}

	if evaluation.Malicious() {
		t.Fatal("fallbackEvaluation() must not hard-block user (Malicious=true)")
	}

	if evaluation.Source != "user_message" {
		t.Fatalf("fallbackEvaluation() source = %q, want %q", evaluation.Source, "user_message")
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

	if !strings.Contains(reasonValue.String(), "singleflight failed") {
		t.Fatalf("fallbackEvaluation() reason = %q, want to contain %q", reasonValue.String(), "singleflight failed")
	}

	sourceValue, ok := handler.attr(record, "source")
	if !ok || sourceValue.String() != "user_message" {
		t.Fatalf("fallbackEvaluation() log source = %q (found=%v), want %q", sourceValue.String(), ok, "user_message")
	}
}
