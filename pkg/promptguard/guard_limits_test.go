package promptguard

import (
	"crypto/sha256"
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
		Version: 3,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "exfil", Family: "prompt_exfil", Type: ruleTypeRegex, Action: "block", View: "joined", Segments: []string{testSegmentPlain}, Pattern: "시스템", Weight: 1.0},
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

	evaluation := evaluateForTest(t, guard, input)
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("oversize decision = %q, want %q", evaluation.Decision, DecisionBlock)
	}

	if !evaluation.OversizeBlocked {
		t.Fatal("oversize evaluation has OversizeBlocked=false, want true")
	}

	if len(evaluation.Hits) != 0 {
		t.Fatalf("oversize hits = %d, want 0 (rule matching must be skipped)", len(evaluation.Hits))
	}

	blockedErr := checkInteractiveForTest(t, guard, input)
	if blockedErr == nil {
		t.Fatal("Check(oversize) error = nil, want *BlockedError")
	}

	blocked, ok := errors.AsType[*BlockedError](blockedErr)
	if !ok {
		t.Fatalf("Check(oversize) error type = %T, want *BlockedError", blockedErr)
	}

	if !slices.Contains(blocked.Rules, ruleInputOversize) {
		t.Fatalf("Check(oversize) BlockedError.Rules = %v, want to contain %q", blocked.Rules, ruleInputOversize)
	}

	if _, err := guard.Check(CheckRequest{Text: input, Source: SourceUserPrompt, Enforcement: EnforcementInteractive}); err == nil {
		t.Fatal("Check(oversize) error = nil, want *BlockedError")
	}
}

func TestSG01GuardOversizeDoesNotInvokeCacheOrSingleflight_cb5f8136(t *testing.T) {
	t.Parallel()

	guard := newOversizeTestGuard(t, nil)

	input := strings.Repeat("시스템", 64)
	if len(input) <= guard.maxInputBytes {
		t.Fatalf("test setup: input len %d must exceed cap %d", len(input), guard.maxInputBytes)
	}

	evaluateForTest(t, guard, input)

	if guard.cache.Len() != 0 {
		t.Fatalf("oversize cache len = %d, want 0 (cache must not be touched)", guard.cache.Len())
	}

	if keys := guard.cacheKeysForTest(); len(keys) != 0 {
		t.Fatalf("oversize cache keys = %v, want none (cacheKey/singleflight must be skipped)", keys)
	}
}

func TestSG01GuardOversizeLogDoesNotIncludePayload_cb5f8136(t *testing.T) {
	t.Parallel()

	handler := &captureHandler{}
	guard := newOversizeTestGuard(t, handler)

	const secretMarker = "SUPERSECRETPAYLOADMARKER"

	input := secretMarker + strings.Repeat("시스템", 64)

	evaluateForTest(t, guard, input)

	if len(handler.records) != 1 {
		t.Fatalf("oversize check emitted %d log records, want 1", len(handler.records))
	}

	record := handler.records[0]
	reason, ok := handler.attr(record, "reason")

	if !ok || !strings.Contains(reason.String(), ruleInputOversize) {
		t.Fatalf("oversize reason = %q (found=%v), want %q", reason.String(), ok, ruleInputOversize)
	}

	if _, ok := handler.attr(record, "size"); !ok {
		t.Fatal("oversize log missing size attribute")
	}

	if _, ok := handler.attr(record, "max"); !ok {
		t.Fatal("oversize log missing max attribute")
	}

	record.Attrs(func(a slog.Attr) bool {
		if strings.Contains(a.Value.String(), secretMarker) {
			t.Fatalf("oversize log attr %q leaked input payload", a.Key)
		}

		return true
	})

	if strings.Contains(record.Message, secretMarker) {
		t.Fatalf("oversize log message leaked input payload: %q", record.Message)
	}
}

func TestCheckUnderCapRunsRuleMatchingAndCaches(t *testing.T) {
	t.Parallel()

	pack, err := compileRulepack(&rawRulepack{
		Version: 3,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "policy", Family: "policy_bypass", Type: ruleTypeRegex, Action: hitActionScore, View: "joined", Segments: []string{testSegmentPlain}, Pattern: "정책[\\s\\S]{0,12}무시", Weight: 0.7},
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

	evaluation := evaluateForTest(t, guard, "정책 무시")
	if evaluation.Decision != DecisionReview {
		t.Fatalf("under-cap evaluation = %#v, want review", evaluation)
	}

	if guard.cache.Len() != 1 {
		t.Fatalf("under-cap cache len = %d, want 1", guard.cache.Len())
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
		Version: 3,
		Policy: rawPolicy{
			BlockThreshold:   1.0,
			ReviewThreshold:  0.55,
			MinBlockFamilies: 2,
		},
		Rules: []rawRule{
			{ID: "policy", Family: "policy_bypass", Type: ruleTypeRegex, Action: hitActionScore, View: "joined", Segments: []string{testSegmentPlain}, Pattern: "정책[\\s\\S]{0,12}무시", Weight: 0.7},
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

	evaluateForTest(t, guard, longA)
	evaluateForTest(t, guard, longB)

	keys := guard.cacheKeysForTest()
	if len(keys) != 2 {
		t.Fatalf("cache key count = %d, want 2 distinct digests", len(keys))
	}

	for _, k := range keys {
		if len(k) != sha256.Size {
			t.Fatalf("cache key len = %d, want %d-byte SHA-256 digest", len(k), sha256.Size)
		}
	}

	if keys[0] == keys[1] {
		t.Fatalf("distinct inputs produced identical cache key %q", keys[0])
	}
}

func TestSegmentBudgetStopsAlternatingLinesAndInlineFragments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "alternating lines", input: strings.Repeat("plain\n> quote\n", maxSegmentBoundaries+1)},
		{name: "inline code", input: strings.Repeat("plain`code`", maxSegmentBoundaries+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			segments, exceeded := buildEvaluationSegments(tc.input)
			if !exceeded || segments != nil {
				t.Fatalf("buildEvaluationSegments() = (%d segments, %v), want bounded rejection", len(segments), exceeded)
			}
		})
	}
}
