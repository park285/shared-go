//go:build !race

package promptguard

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/url"
	"testing"
)

func TestPromptGuardAllocationCeilings(t *testing.T) {
	payload := "ordinary synthetic payload that does not contain an instruction"
	tests := []struct {
		name      string
		input     string
		maxAllocs float64
	}{
		{
			name:      "decoder heavy",
			input:     base64.StdEncoding.EncodeToString([]byte(url.PathEscape(payload))),
			maxAllocs: 64,
		},
		{
			name:      "short rule context",
			input:     "aWdub3Jl previous instructions",
			maxAllocs: 20,
		},
		{
			name:      "rolling aggregate",
			input:     rollingAggregateBenchmarkInput(),
			maxAllocs: 840,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := newAllocationTestGuard(t)
			allocs := testing.AllocsPerRun(20, func() {
				_ = guard.evaluateRaw(tt.input)
			})
			if allocs > tt.maxAllocs {
				t.Fatalf("evaluateRaw() allocations = %.0f, want <= %.0f", allocs, tt.maxAllocs)
			}
		})
	}
}

func newAllocationTestGuard(t *testing.T) *Guard {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	guard, err := NewGuard(Config{Enabled: true, UseEmbeddedDefaults: true}, logger)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}
	return guard
}
