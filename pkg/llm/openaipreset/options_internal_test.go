package openaipreset

import "testing"

func TestWithReasoningEffort_TrimsSurroundingWhitespace(t *testing.T) {
	cfg := &config{}
	WithReasoningEffort("  high  ")(cfg)

	if cfg.reasoningEffort != "high" {
		t.Fatalf("reasoningEffort = %q, want %q", cfg.reasoningEffort, "high")
	}
}
