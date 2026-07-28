package httputil

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkFixedWindowAllowHotIdentity(b *testing.B) {
	for _, cardinality := range []int{1, 10000} {
		b.Run(fmt.Sprintf("%d", cardinality), func(b *testing.B) {
			now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			limiter := NewFixedWindowRateLimiter(b.N+cardinality+1, time.Hour, FixedWindowOptions{
				MaxIdentities: cardinality,
				EntryTTL:      time.Hour,
				Now:           func() time.Time { return now },
			})
			for i := range cardinality {
				limiter.Allow(fmt.Sprintf("identity-%05d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
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
	for i := range b.N {
		limiter.Allow(fmt.Sprintf("churn-%08d", i))
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
			for range b.N {
				limiter.IsAllowed("unseen-at-capacity")
			}
		})
	}
}
