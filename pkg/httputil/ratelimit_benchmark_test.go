package httputil

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func BenchmarkFixedWindowAllowHotIdentity(b *testing.B) {
	for _, cardinality := range []int{1, 10000} {
		b.Run(fmt.Sprintf("%d", cardinality), func(b *testing.B) {
			now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			limiter := NewFixedWindowRateLimiter(math.MaxInt, time.Hour, FixedWindowOptions{
				MaxIdentities: cardinality,
				EntryTTL:      time.Hour,
				Now:           func() time.Time { return now },
			})

			for i := range cardinality {
				limiter.Allow(fmt.Sprintf("identity-%05d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				limiter.Allow("identity-00000")
			}
		})
	}
}

func BenchmarkFixedWindowAllowUniqueAtCapacity(b *testing.B) {
	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		MaxIdentities: 10000,
		EntryTTL:      time.Hour,
		Now:           func() time.Time { return now },
	})

	for i := range 10000 {
		limiter.Allow(fmt.Sprintf("prefill-%05d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()

	i := 0
	for b.Loop() {
		limiter.Allow(fmt.Sprintf("churn-%08d", i))
		i++
	}
}

func BenchmarkLoginFailureIsAllowedSaturatedMiss(b *testing.B) {
	for _, cardinality := range []int{1, 10000} {
		b.Run(fmt.Sprintf("identities-%d", cardinality), func(b *testing.B) {
			now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
				MaxIdentities: cardinality,
				Window:        time.Hour,
				Now:           func() time.Time { return now },
			})

			for i := range cardinality {
				limiter.IsAllowed(fmt.Sprintf("prefill-%05d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				limiter.IsAllowed("unseen-at-capacity")
			}
		})
	}
}
