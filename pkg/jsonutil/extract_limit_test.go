package jsonutil

import (
	"errors"
	"strings"
	"testing"
)

func TestSG03ExtractRejectsInputLargerThanDefaultLimit_39575489(t *testing.T) {
	t.Parallel()

	oversize := strings.Repeat("a", DefaultExtractMaxBytes+1)

	if _, err := Extract(oversize); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("Extract(oversize) error = %v, want ErrInputTooLarge", err)
	}
}

func TestSG03ExtractAcceptsInputAtDefaultLimit_39575489(t *testing.T) {
	t.Parallel()

	payload := `{"answer":"yes"}`
	pad := strings.Repeat(" ", DefaultExtractMaxBytes-len(payload))
	atLimit := pad + payload
	if len(atLimit) != DefaultExtractMaxBytes {
		t.Fatalf("test setup: input len %d, want exactly %d", len(atLimit), DefaultExtractMaxBytes)
	}

	got, err := Extract(atLimit)
	if err != nil {
		t.Fatalf("Extract(at limit) error = %v, want nil", err)
	}
	if string(got) != payload {
		t.Fatalf("Extract(at limit) = %q, want %q", got, payload)
	}
}

func BenchmarkSG03ExtractLargeNoJSONLinear_39575489(b *testing.B) {
	input := strings.Repeat("x", DefaultExtractMaxBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = Extract(input)
	}
}
