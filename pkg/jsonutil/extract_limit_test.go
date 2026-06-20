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
	if _, err := ExtractToMap(oversize); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("ExtractToMap(oversize) error = %v, want ErrInputTooLarge", err)
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

func TestSG03ExtractWithLimitAcceptsExplicitLimit_39575489(t *testing.T) {
	t.Parallel()

	payload := `{"k":"v"}`
	big := strings.Repeat(" ", DefaultExtractMaxBytes*2) + payload

	if _, err := Extract(big); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("Extract(big) error = %v, want ErrInputTooLarge (default cap)", err)
	}

	got, err := ExtractWithLimit(big, len(big))
	if err != nil {
		t.Fatalf("ExtractWithLimit(big, explicit) error = %v, want nil", err)
	}
	if string(got) != payload {
		t.Fatalf("ExtractWithLimit(big, explicit) = %q, want %q", got, payload)
	}

	m, err := ExtractToMapWithLimit(big, len(big))
	if err != nil {
		t.Fatalf("ExtractToMapWithLimit(big, explicit) error = %v, want nil", err)
	}
	if m["k"] != "v" {
		t.Fatalf("ExtractToMapWithLimit map = %v, want k=v", m)
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
