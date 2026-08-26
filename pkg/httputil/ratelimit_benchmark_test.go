package httputil

import (
	"fmt"
	"math"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

var (
	benchmarkAllowedSink  bool
	benchmarkClientIPSink string
	benchmarkHashSink     string
)

func BenchmarkFixedWindowAllowHotIdentity(b *testing.B) {
	for _, cardinality := range []int{1, 10000} {
		b.Run(fmt.Sprintf("identities-%d", cardinality), func(b *testing.B) {
			now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			limiter := NewFixedWindowRateLimiter(math.MaxInt, time.Hour, FixedWindowOptions{
				MaxIdentities: cardinality,
				EntryTTL:      time.Hour,
				Now:           func() time.Time { return now },
			})

			for _, identity := range benchmarkIdentities(cardinality, "identity") {
				limiter.Allow(identity)
			}

			b.ReportAllocs()

			for b.Loop() {
				benchmarkAllowedSink = limiter.Allow("identity-00000")
			}
		})
	}
}

func BenchmarkFixedWindowAllowParallel(b *testing.B) {
	for _, cardinality := range []int{1, 10, 1000} {
		b.Run(fmt.Sprintf("identities-%d", cardinality), func(b *testing.B) {
			now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			identities := benchmarkIdentities(cardinality, "parallel")
			limiter := NewFixedWindowRateLimiter(math.MaxInt, time.Hour, FixedWindowOptions{
				MaxIdentities: cardinality,
				EntryTTL:      time.Hour,
				Now:           func() time.Time { return now },
			})

			for _, identity := range identities {
				limiter.Allow(identity)
			}

			var nextWorker atomic.Int64

			b.ReportAllocs()
			b.RunParallel(func(parallel *testing.PB) {
				index := (nextWorker.Add(1) - 1) % int64(len(identities))

				for parallel.Next() {
					_ = limiter.Allow(identities[index])
					index++

					if index == int64(len(identities)) {
						index = 0
					}
				}
			})
		})
	}
}

func BenchmarkFixedWindowAllowCapacityChurn(b *testing.B) {
	const capacity = 10000

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	identities := benchmarkIdentities(capacity*2, "churn")
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		MaxIdentities: capacity,
		EntryTTL:      time.Hour,
		Now:           func() time.Time { return now },
	})

	for _, identity := range benchmarkIdentities(capacity, "prefill") {
		limiter.Allow(identity)
	}

	index := 0

	b.ReportAllocs()

	for b.Loop() {
		benchmarkAllowedSink = limiter.Allow(identities[index])
		index++

		if index == len(identities) {
			index = 0
		}
	}
}

func BenchmarkFixedWindowAllowCapacityChurnEndToEnd(b *testing.B) {
	const capacity = 10000

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		MaxIdentities: capacity,
		EntryTTL:      time.Hour,
		Now:           func() time.Time { return now },
	})

	for _, identity := range benchmarkIdentities(capacity, "prefill") {
		limiter.Allow(identity)
	}

	i := 0

	b.ReportAllocs()

	for b.Loop() {
		benchmarkAllowedSink = limiter.Allow(fmt.Sprintf("churn-%08d", i))
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

			for _, identity := range benchmarkIdentities(cardinality, "prefill") {
				limiter.IsAllowed(identity)
			}

			b.ReportAllocs()

			for b.Loop() {
				benchmarkAllowedSink, _ = limiter.IsAllowed("unseen-at-capacity")
			}
		})
	}
}

func BenchmarkLoginFailureIsAllowedExpiryCleanup(b *testing.B) {
	for _, cardinality := range []int{128, 10000} {
		b.Run(fmt.Sprintf("identities-%d", cardinality), func(b *testing.B) {
			start := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
			identities := benchmarkIdentities(cardinality, "expired")

			b.ReportAllocs()

			for b.Loop() {
				b.StopTimer()

				now := start
				limiter := NewLoginFailureRateLimiter(LoginFailureRateLimiterOptions{
					MaxIdentities: cardinality,
					Window:        time.Hour,
					Now:           func() time.Time { return now },
				})

				for _, identity := range identities {
					limiter.IsAllowed(identity)
				}

				now = start.Add(time.Hour)

				b.StartTimer()

				benchmarkAllowedSink, _ = limiter.IsAllowed("after-expiry")
			}
		})
	}
}

func BenchmarkClientIPForwarded(b *testing.B) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		b.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	const forwarded = "198.51.100.1, 198.51.100.2, 198.51.100.3, 10.0.0.1, 10.0.0.2, 10.0.0.3"

	for _, benchmark := range []struct {
		name string
		mode ForwardedHeaderMode
	}{
		{name: "leftmost", mode: ForwardedHeaderLeftmost},
		{name: "rightmost_non_trusted", mode: ForwardedHeaderRightmostNonTrusted},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			request := &http.Request{
				RemoteAddr: "10.1.2.3:1234",
				Header: http.Header{
					"X-Forwarded-For": []string{forwarded},
				},
			}
			options := ClientIPOptions{
				TrustForwarded: true,
				TrustedProxies: trusted,
				ForwardedMode:  benchmark.mode,
			}

			b.ReportAllocs()

			for b.Loop() {
				benchmarkClientIPSink = ClientIP(request, options)
			}
		})
	}
}

func BenchmarkRateLimitKeyHash(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		benchmarkHashSink = RateLimitKeyHash("some-api-key-with-enough-entropy")
	}
}

func benchmarkIdentities(count int, prefix string) []string {
	identities := make([]string, count)
	for index := range count {
		identities[index] = fmt.Sprintf("%s-%05d", prefix, index)
	}

	return identities
}
