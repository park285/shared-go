package backoff

import (
	"testing"
	"time"
)

func TestNextExponentialBackoff_InitialFromZero(t *testing.T) {
	got := NextExponentialBackoff(0, time.Minute, 5*time.Second)
	if got != 5*time.Second {
		t.Fatalf("NextExponentialBackoff() = %v, want %v", got, 5*time.Second)
	}
}

func TestNextExponentialBackoff_Doubles(t *testing.T) {
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{name: "from step", current: 5 * time.Second, want: 10 * time.Second},
		{name: "from larger value", current: 20 * time.Second, want: 40 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextExponentialBackoff(tt.current, time.Minute, 5*time.Second)
			if got != tt.want {
				t.Fatalf("NextExponentialBackoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextExponentialBackoff_CapsAtMax(t *testing.T) {
	got := NextExponentialBackoff(40*time.Second, time.Minute, 5*time.Second)
	if got != time.Minute {
		t.Fatalf("NextExponentialBackoff() = %v, want %v", got, time.Minute)
	}
}

func TestNextExponentialBackoff_StepFloor(t *testing.T) {
	got := NextExponentialBackoff(time.Second, time.Minute, 5*time.Second)
	if got != 5*time.Second {
		t.Fatalf("NextExponentialBackoff() = %v, want %v", got, 5*time.Second)
	}
}

func TestComputeExponentialBackoff_AttemptGrowth(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "attempt zero", attempt: 0, want: 2 * time.Second},
		{name: "attempt one", attempt: 1, want: 4 * time.Second},
		{name: "attempt three", attempt: 3, want: 16 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeExponentialBackoff(tt.attempt, 2*time.Second, time.Minute, 0)
			if got != tt.want {
				t.Fatalf("ComputeExponentialBackoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeExponentialBackoff_CapsAtMax(t *testing.T) {
	got := ComputeExponentialBackoff(10, 2*time.Second, time.Minute, 0)
	if got != time.Minute {
		t.Fatalf("ComputeExponentialBackoff() = %v, want %v", got, time.Minute)
	}
}

func TestComputeExponentialBackoff_JitterRange(t *testing.T) {
	base := 2 * time.Second
	jitter := 150 * time.Millisecond

	for range 100 {
		got := ComputeExponentialBackoff(0, base, time.Minute, jitter)
		if got < base || got >= base+jitter {
			t.Fatalf("ComputeExponentialBackoff() = %v, want in [%v, %v)", got, base, base+jitter)
		}
	}
}

func TestComputeExponentialBackoff_JitterUpperBoundExclusive(t *testing.T) {
	base := 2 * time.Second
	jitter := time.Nanosecond

	for range 100 {
		got := ComputeExponentialBackoff(0, base, time.Minute, jitter)
		if got >= base+jitter {
			t.Fatalf("ComputeExponentialBackoff() = %v, want < %v", got, base+jitter)
		}
	}
}

func TestComputeExponentialBackoff_ZeroJitter(t *testing.T) {
	base := 2 * time.Second

	got := ComputeExponentialBackoff(0, base, time.Minute, 0)
	if got != base {
		t.Fatalf("ComputeExponentialBackoff() = %v, want %v", got, base)
	}
}

func TestComputeExponentialBackoff_NegativeAttempt(t *testing.T) {
	got := ComputeExponentialBackoff(-1, 2*time.Second, time.Minute, 0)
	if got != 0 {
		t.Fatalf("ComputeExponentialBackoff() = %v, want 0", got)
	}
}

func TestComputeExponentialBackoffHalfJitter_RangeIsHalfToFullOfCappedBase(t *testing.T) {
	base := 2 * time.Second
	maxInterval := 10 * time.Second
	tests := []struct {
		name        string
		attempt     int
		expectedCap time.Duration
	}{
		{name: "attempt zero", attempt: 0, expectedCap: 2 * time.Second},
		{name: "attempt one", attempt: 1, expectedCap: 4 * time.Second},
		{name: "attempt two", attempt: 2, expectedCap: 8 * time.Second},
		{name: "attempt three capped", attempt: 3, expectedCap: 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lowerBound := tt.expectedCap / 2
			for range 1000 {
				got := ComputeExponentialBackoffHalfJitter(tt.attempt, base, maxInterval)
				if got < lowerBound || got >= tt.expectedCap {
					t.Fatalf("ComputeExponentialBackoffHalfJitter() = %v, want in [%v, %v)", got, lowerBound, tt.expectedCap)
				}
			}
		})
	}
}

func TestComputeExponentialBackoffHalfJitter_OddCapCoversFullRange(t *testing.T) {
	base := 5 * time.Nanosecond
	maxInterval := 5 * time.Nanosecond
	expectedCap := 5 * time.Nanosecond
	seen := make(map[time.Duration]bool)

	for range 1000 {
		got := ComputeExponentialBackoffHalfJitter(0, base, maxInterval)
		if got < expectedCap/2 || got >= expectedCap {
			t.Fatalf("ComputeExponentialBackoffHalfJitter() = %v, want in [%v, %v)", got, expectedCap/2, expectedCap)
		}
		seen[got] = true
	}

	for _, want := range []time.Duration{expectedCap / 2, expectedCap/2 + 1, expectedCap - 1} {
		if !seen[want] {
			t.Fatalf("ComputeExponentialBackoffHalfJitter() never returned %v; seen %v", want, seen)
		}
	}
}

func TestComputeExponentialBackoffHalfJitter_TinyCapDoesNotPanic(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
	}{
		{name: "attempt zero", attempt: 0},
		{name: "capped later attempt", attempt: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeExponentialBackoffHalfJitter(tt.attempt, time.Nanosecond, time.Nanosecond)
			if got != time.Nanosecond {
				t.Fatalf("ComputeExponentialBackoffHalfJitter() = %v, want %v", got, time.Nanosecond)
			}
		})
	}
}

func TestComputeExponentialBackoffHalfJitter_InvalidInputsReturnZero(t *testing.T) {
	tests := []struct {
		name        string
		attempt     int
		base        time.Duration
		maxInterval time.Duration
	}{
		{name: "negative attempt", attempt: -1, base: time.Second, maxInterval: time.Minute},
		{name: "zero base", attempt: 0, base: 0, maxInterval: time.Minute},
		{name: "zero max interval", attempt: 0, base: time.Second, maxInterval: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeExponentialBackoffHalfJitter(tt.attempt, tt.base, tt.maxInterval)
			if got != 0 {
				t.Fatalf("ComputeExponentialBackoffHalfJitter() = %v, want 0", got)
			}
		})
	}
}
