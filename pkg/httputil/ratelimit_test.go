package httputil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimitKeyHashPreservesFullDigestPrefix(t *testing.T) {
	t.Parallel()

	const value = "some-api-key"

	sum := sha256.Sum256([]byte(value))
	want := hex.EncodeToString(sum[:])[:16]

	if got := RateLimitKeyHash(value); got != want {
		t.Fatalf("RateLimitKeyHash() = %q, want %q", got, want)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	t.Parallel()

	got, err := ParseTrustedProxies([]string{"10.0.0.0/8", " 192.0.2.10 ", "", "2001:db8::1"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("ParseTrustedProxies() len = %d, want 3", len(got))
	}

	if got[1].String() != "192.0.2.10/32" {
		t.Fatalf("single IPv4 prefix = %s, want 192.0.2.10/32", got[1])
	}

	if got[2].String() != "2001:db8::1/128" {
		t.Fatalf("single IPv6 prefix = %s, want 2001:db8::1/128", got[2])
	}
}

func TestParseTrustedProxyCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantLen int
		wantErr bool
	}{
		{name: "empty", raw: "", wantLen: 0},
		{name: "cidrs", raw: "10.0.0.0/8, ,192.168.0.0/16", wantLen: 2},
		{name: "invalid", raw: "not-a-cidr", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseTrustedProxyCSV(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseTrustedProxyCSV() error = nil, want error")
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseTrustedProxyCSV() error = %v", err)
			}

			if len(got) != tt.wantLen {
				t.Fatalf("ParseTrustedProxyCSV() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestClientIPTrustedForwardedModes(t *testing.T) {
	t.Parallel()

	for _, tt := range clientIPTrustedForwardedCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ClientIP(tt.req(), tt.opts); got != tt.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

type clientIPTrustedForwardedCase struct {
	name string
	req  func() *http.Request
	opts ClientIPOptions
	want string
}

func clientIPTrustedForwardedCases(t *testing.T) []clientIPTrustedForwardedCase {
	t.Helper()

	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	return []clientIPTrustedForwardedCase{
		{
			name: "untrusted peer ignores xff",
			req:  forwardedHeaderRequest(t, "203.0.113.50:4321", "X-Forwarded-For", "198.51.100.7"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted},
			want: "203.0.113.50",
		},
		{
			name: "leftmost mode matches twentyq",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Forwarded-For", "203.0.113.7, 10.0.0.1"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted, ForwardedMode: ForwardedHeaderLeftmost},
			want: "203.0.113.7",
		},
		{
			name: "leftmost mode accepts host port xff hop",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Forwarded-For", "203.0.113.7:1234, 10.0.0.1"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted, ForwardedMode: ForwardedHeaderLeftmost},
			want: "203.0.113.7",
		},
		{
			name: "rightmost non trusted mode matches admin dashboard",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Forwarded-For", "198.51.100.8, 203.0.113.7, 10.0.0.1"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted, ForwardedMode: ForwardedHeaderRightmostNonTrusted},
			want: "203.0.113.7",
		},
		{
			name: "rightmost mode rejects host port xff hop",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Forwarded-For", "203.0.113.7:1234, 10.0.0.1"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted, ForwardedMode: ForwardedHeaderRightmostNonTrusted},
			want: "10.1.2.3",
		},
		{
			name: "forwarding disabled uses peer",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Forwarded-For", "203.0.113.7"),
			opts: ClientIPOptions{TrustForwarded: false, TrustedProxies: trusted},
			want: "10.1.2.3",
		},
		{
			name: "x real ip fallback",
			req:  forwardedHeaderRequest(t, testValue101234, "X-Real-IP", "203.0.113.9"),
			opts: ClientIPOptions{TrustForwarded: true, TrustedProxies: trusted},
			want: "203.0.113.9",
		},
	}
}

func forwardedHeaderRequest(t *testing.T, remoteAddr, header, value string) func() *http.Request {
	t.Helper()

	return func() *http.Request {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

		req.RemoteAddr = remoteAddr
		req.Header.Set(header, value)

		return req
	}
}

func TestRateLimitIdentity(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	req.RemoteAddr = "198.51.100.10:1234"

	if got := RateLimitIdentity(req, "same-key", ClientIPOptions{}); !strings.HasPrefix(got, "key:") {
		t.Fatalf("RateLimitIdentity(api key) = %q, want key prefix", got)
	}

	if got := RateLimitIdentity(req, "", ClientIPOptions{}); got != "ip:198.51.100.10" {
		t.Fatalf("RateLimitIdentity(ip) = %q, want ip:198.51.100.10", got)
	}

	if got := RateLimitIdentity(nil, "", ClientIPOptions{}); got != "ip:unknown" {
		t.Fatalf("RateLimitIdentity(nil) = %q, want ip:unknown", got)
	}
}

func TestFixedWindowRateLimiterAllow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(2, time.Minute, FixedWindowOptions{
		Now: func() time.Time { return now },
	})

	if !limiter.Allow("admin-1") {
		t.Fatal("first Allow() = false")
	}

	if !limiter.Allow("admin-1") {
		t.Fatal("second Allow() = false")
	}

	if limiter.Allow("admin-1") {
		t.Fatal("third Allow() = true, want false")
	}

	now = now.Add(time.Minute)

	if !limiter.Allow("admin-1") {
		t.Fatal("Allow() after window reset = false")
	}

	if limiter.Allow(" ") {
		t.Fatal("Allow(empty identity) = true, want false")
	}

	if !(*FixedWindowRateLimiter)(nil).Allow("any") {
		t.Fatal("nil limiter Allow() = false, want true")
	}
}

func TestFixedWindowRateLimiterTTLAndEviction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		MaxIdentities: 1,
		EntryTTL:      time.Minute,
		Now:           func() time.Time { return now },
	})

	if !limiter.Allow("first") {
		t.Fatal("Allow(first) = false")
	}

	if !limiter.Allow("second") {
		t.Fatal("Allow(second) = false")
	}

	if !limiter.Allow("first") {
		t.Fatal("Allow(first after eviction) = false")
	}

	now = now.Add(time.Hour)

	if !limiter.Allow("first") {
		t.Fatal("Allow(first after ttl) = false")
	}
}

func TestFixedWindowRateLimiterClampsEntryTTLToWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		EntryTTL: time.Second,
		Now:      func() time.Time { return now },
	})

	if !limiter.Allow("session") {
		t.Fatal("first Allow(session) = false")
	}

	now = now.Add(time.Second)

	if limiter.Allow("session") {
		t.Fatal("Allow(session) after configured short ttl = true, want quota preserved")
	}

	now = now.Add(time.Hour - time.Second)

	if !limiter.Allow("session") {
		t.Fatal("Allow(session) at window boundary = false")
	}
}

func TestFixedWindowRateLimiterEvictsLeastRecentlySeenInConstantOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(2, time.Hour, FixedWindowOptions{
		MaxIdentities: 2,
		EntryTTL:      time.Hour,
		Now:           func() time.Time { return now },
	})

	if !limiter.Allow("first") || !limiter.Allow("second") {
		t.Fatal("initial identities were not admitted")
	}

	if !limiter.Allow("first") {
		t.Fatal("touching first identity failed")
	}

	if !limiter.Allow("third") {
		t.Fatal("third identity was not admitted after eviction")
	}

	limiter.mu.Lock()

	_, hasFirst := limiter.entries["first"]
	_, hasSecond := limiter.entries["second"]
	_, hasThird := limiter.entries["third"]
	limiter.mu.Unlock()

	if !hasFirst || hasSecond || !hasThird {
		t.Fatalf("entries after LRU eviction = first:%t second:%t third:%t", hasFirst, hasSecond, hasThird)
	}
}

func TestFixedWindowRateLimiterConcurrentHotIdentity(t *testing.T) {
	t.Parallel()

	const requests = 128

	limiter := NewFixedWindowRateLimiter(requests, time.Minute, FixedWindowOptions{})
	results := make(chan bool, requests)

	var wg sync.WaitGroup

	for range requests {
		wg.Go(func() {
			results <- limiter.Allow("shared")
		})
	}

	wg.Wait()
	close(results)

	for allowed := range results {
		if !allowed {
			t.Fatal("one of the in-window requests was rejected")
		}
	}

	if limiter.Allow("shared") {
		t.Fatal("request beyond concurrent quota was allowed")
	}
}

func TestFixedWindowRateLimiterZeroOptionsApplySafeDefaults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		Now: func() time.Time { return now },
	})

	for i := range defaultFixedWindowMaxIdentities + 1 {
		if !limiter.Allow(fmt.Sprintf("identity-%05d", i)) {
			t.Fatalf("Allow(identity-%05d) = false", i)
		}

		now = now.Add(time.Nanosecond)
	}

	limiter.mu.Lock()

	entryCount := len(limiter.entries)
	_, hasFirst := limiter.entries["identity-00000"]
	_, hasNewest := limiter.entries[fmt.Sprintf("identity-%05d", defaultFixedWindowMaxIdentities)]
	limiter.mu.Unlock()

	if entryCount != defaultFixedWindowMaxIdentities {
		t.Fatalf("entry count = %d, want %d", entryCount, defaultFixedWindowMaxIdentities)
	}

	if hasFirst {
		t.Fatal("oldest identity was not evicted")
	}

	if !hasNewest {
		t.Fatal("newest identity was evicted")
	}

	ttlLimiter := NewFixedWindowRateLimiter(1, time.Hour, FixedWindowOptions{
		Now: func() time.Time { return now },
	})
	if !ttlLimiter.Allow("session") {
		t.Fatal("Allow(session) = false")
	}

	now = now.Add(defaultFixedWindowEntryTTL)

	if ttlLimiter.Allow("session") {
		t.Fatal("Allow(session after clamped default ttl) = true, want quota preserved until window")
	}
}

func TestFixedWindowRateLimitMiddleware(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
	limiter := NewFixedWindowRateLimiter(1, time.Minute, FixedWindowOptions{
		Now: func() time.Time { return now },
	})
	handler := FixedWindowRateLimitMiddleware(RateLimitMiddlewareConfig{
		Limiter: limiter,
		Identity: func(r *http.Request) string {
			return RateLimitIdentity(r, APIKeyFromRequest(r), ClientIPOptions{})
		},
		Reject: WriteRateLimitExceededJSON,
		Skip: func(r *http.Request) bool {
			return r.Method == http.MethodOptions
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	first.RemoteAddr = "198.51.100.10:12000"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, first)

	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", rec.Code)
	}

	second := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	second.RemoteAddr = "198.51.100.10:13000"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, second)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec.Code)
	}

	optionsReq := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/", http.NoBody)

	optionsReq.RemoteAddr = "198.51.100.10:14000"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, optionsReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("OPTIONS status = %d, want 200", rec.Code)
	}
}
