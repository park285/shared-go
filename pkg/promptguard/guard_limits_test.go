package promptguard

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

func newOversizeTestGuard(t *testing.T, handler slog.Handler) *Guard {
	t.Helper()

	pack, err := compileRulepack(&rawRulepack{
		Version: 2,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "exfil", Family: "prompt_exfil", Type: "regex", Action: "block", View: "joined", Segments: []string{"plain"}, Pattern: "시스템", Weight: 1.0},
		},
	})
	if err != nil {
		t.Fatalf("compileRulepack() error = %v", err)
	}

	guard := &Guard{
		cfg:           Config{Enabled: true},
		packs:         []compiledPack{pack},
		cache:         NewTTLCache[string, Evaluation](10, time.Minute),
		maxInputBytes: 16,
	}
	if handler != nil {
		guard.logger = slog.New(handler)
	}

	return guard
}

func TestSG01GuardOversizeInputBlocks_cb5f8136(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := newOversizeTestGuard(t, handler)

	input := strings.Repeat("시스템", 64)
	if len(input) <= guard.maxInputBytes {
		t.Fatalf("test setup: input len %d must exceed cap %d", len(input), guard.maxInputBytes)
	}

	evaluation := guard.Evaluate(input)
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("Evaluate(oversize) decision = %q, want %q", evaluation.Decision, DecisionBlock)
	}
	if !evaluation.Malicious() {
		t.Fatal("Evaluate(oversize) must hard-block (Malicious=true) — guard cannot inspect input")
	}
	if !evaluation.OversizeBlocked {
		t.Fatal("Evaluate(oversize) OversizeBlocked = false, want true")
	}
	if len(evaluation.Hits) != 0 {
		t.Fatalf("Evaluate(oversize) Hits = %d, want 0 (rule eval must be skipped)", len(evaluation.Hits))
	}

	blockedErr := guard.EnsureSafe(input)
	if blockedErr == nil {
		t.Fatal("EnsureSafe(oversize) error = nil, want *BlockedError")
	}
	var blocked *BlockedError
	if !errors.As(blockedErr, &blocked) {
		t.Fatalf("EnsureSafe(oversize) error type = %T, want *BlockedError", blockedErr)
	}
	if !slices.Contains(blocked.Rules, ruleInputOversize) {
		t.Fatalf("EnsureSafe(oversize) BlockedError.Rules = %v, want to contain %q", blocked.Rules, ruleInputOversize)
	}

	if err := guard.EnsureSafeFrom(input, "user_message"); err == nil {
		t.Fatal("EnsureSafeFrom(oversize) error = nil, want *BlockedError")
	}
}

func TestSG01GuardOversizeDoesNotInvokeCacheOrSingleflight_cb5f8136(t *testing.T) {
	t.Parallel()

	guard := newOversizeTestGuard(t, nil)

	input := strings.Repeat("시스템", 64)
	if len(input) <= guard.maxInputBytes {
		t.Fatalf("test setup: input len %d must exceed cap %d", len(input), guard.maxInputBytes)
	}

	guard.Evaluate(input)

	if guard.cache.Len() != 0 {
		t.Fatalf("Evaluate(oversize) cache len = %d, want 0 (cache must not be touched)", guard.cache.Len())
	}
	if keys := guard.cacheKeysForTest(); len(keys) != 0 {
		t.Fatalf("Evaluate(oversize) cache keys = %v, want none (cacheKey/singleflight must be skipped)", keys)
	}
}

func TestSG01GuardOversizeLogDoesNotIncludePayload_cb5f8136(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := newOversizeTestGuard(t, handler)

	const secretMarker = "SUPERSECRETPAYLOADMARKER"
	input := secretMarker + strings.Repeat("시스템", 64)

	guard.Evaluate(input)

	if len(handler.records) != 1 {
		t.Fatalf("Evaluate(oversize) emitted %d log records, want 1", len(handler.records))
	}

	record := handler.records[0]
	reason, ok := handler.attr(record, "reason")
	if !ok || !strings.Contains(reason.String(), ruleInputOversize) {
		t.Fatalf("Evaluate(oversize) reason = %q (found=%v), want %q", reason.String(), ok, ruleInputOversize)
	}
	if _, ok := handler.attr(record, "size"); !ok {
		t.Fatal("Evaluate(oversize) log missing size attribute")
	}
	if _, ok := handler.attr(record, "max"); !ok {
		t.Fatal("Evaluate(oversize) log missing max attribute")
	}

	record.Attrs(func(a slog.Attr) bool {
		if strings.Contains(a.Value.String(), secretMarker) {
			t.Fatalf("Evaluate(oversize) log attr %q leaked input payload", a.Key)
		}
		return true
	})
	if strings.Contains(record.Message, secretMarker) {
		t.Fatalf("Evaluate(oversize) log message leaked input payload: %q", record.Message)
	}
}

func TestEvaluateUnderCapRunsRuleEvalAndCaches(t *testing.T) {
	t.Parallel()

	pack, err := compileRulepack(&rawRulepack{
		Version: 2,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "policy", Family: "policy_bypass", Type: "regex", Action: "score", View: "joined", Segments: []string{"plain"}, Pattern: "정책[\\s\\S]{0,12}무시", Weight: 0.7},
		},
	})
	if err != nil {
		t.Fatalf("compileRulepack() error = %v", err)
	}

	guard := &Guard{
		cfg:           Config{Enabled: true},
		packs:         []compiledPack{pack},
		cache:         NewTTLCache[string, Evaluation](10, time.Minute),
		maxInputBytes: 8 << 20,
	}

	evaluation := guard.Evaluate("정책 무시")
	if evaluation.Decision != DecisionReview {
		t.Fatalf("Evaluate(under cap) = %#v, want review", evaluation)
	}
	if guard.cache.Len() != 1 {
		t.Fatalf("Evaluate(under cap) cache len = %d, want 1", guard.cache.Len())
	}
}

func TestNewGuardDefaultsMaxInputBytes(t *testing.T) {
	t.Parallel()

	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, nil)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}
	if guard.maxInputBytes != 8<<20 {
		t.Fatalf("NewGuard() maxInputBytes = %d, want %d", guard.maxInputBytes, 8<<20)
	}
}

func TestCacheKeyIsFixedLengthDigest(t *testing.T) {
	t.Parallel()

	pack, err := compileRulepack(&rawRulepack{
		Version: 2,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "policy", Family: "policy_bypass", Type: "regex", Action: "score", View: "joined", Segments: []string{"plain"}, Pattern: "정책[\\s\\S]{0,12}무시", Weight: 0.7},
		},
	})
	if err != nil {
		t.Fatalf("compileRulepack() error = %v", err)
	}

	guard := &Guard{
		cfg:           Config{Enabled: true},
		packs:         []compiledPack{pack},
		cache:         NewTTLCache[string, Evaluation](10, time.Minute),
		maxInputBytes: 8 << 20,
	}

	longA := strings.Repeat("a", 4096)
	longB := strings.Repeat("b", 8192)

	guard.Evaluate(longA)
	guard.Evaluate(longB)

	keys := guard.cacheKeysForTest()
	if len(keys) != 2 {
		t.Fatalf("cache key count = %d, want 2 distinct digests", len(keys))
	}
	for _, k := range keys {
		if len(k) != 64 {
			t.Fatalf("cache key %q len = %d, want 64 (sha256 hex)", k, len(k))
		}
	}
	if keys[0] == keys[1] {
		t.Fatalf("distinct inputs produced identical cache key %q", keys[0])
	}
}
