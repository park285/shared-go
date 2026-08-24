package backoff

import (
	"testing"
	"time"
)

func BenchmarkBackoffHalfJitter(b *testing.B) {
	const (
		base        = 100 * time.Millisecond
		maxInterval = 30 * time.Second
	)

	b.ReportAllocs()

	attempt := 0

	for b.Loop() {
		_ = ComputeExponentialBackoffHalfJitter(attempt%8, base, maxInterval)
		attempt++
	}
}
