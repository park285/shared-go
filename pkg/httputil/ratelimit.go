package httputil

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	defaultFixedWindowMaxIdentities = 10000
	defaultFixedWindowEntryTTL      = 2 * time.Minute
)

type ClientIPOptions struct {
	TrustForwarded bool
	TrustedProxies []netip.Prefix
}

type IdentityFunc func(*http.Request) string

type RateLimitRejectFunc func(http.ResponseWriter, *http.Request, string)

type FixedWindowOptions struct {
	MaxIdentities int
	EntryTTL      time.Duration
	Now           func() time.Time
}

type FixedWindowRateLimiter struct {
	mu            sync.Mutex
	limit         int
	window        time.Duration
	maxIdentities int
	entryTTL      time.Duration
	now           func() time.Time
	entries       map[string]*fixedWindowEntry
	lru           list.List
}

type fixedWindowEntry struct {
	identity    string
	windowStart time.Time
	lastSeen    time.Time
	count       int
	element     *list.Element
}

type RateLimitMiddlewareConfig struct {
	Limiter  *FixedWindowRateLimiter
	Identity IdentityFunc
	Reject   RateLimitRejectFunc
	Skip     func(*http.Request) bool
}

func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, fmt.Errorf("httputil: parse prefix: %w", err)
			}

			result = append(result, prefix.Masked())

			continue
		}

		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("httputil: parse addr: %w", err)
		}

		bits := 32

		if addr.Is6() {
			bits = 128
		}

		result = append(result, netip.PrefixFrom(addr, bits).Masked())
	}

	return result, nil
}

func clientIP(r *http.Request, opts ClientIPOptions) string {
	if r == nil {
		return ""
	}

	remoteIP, ok := parseIPCandidate(r.RemoteAddr)
	if !ok {
		return ""
	}

	if opts.TrustForwarded && isTrustedProxy(remoteIP, opts.TrustedProxies) {
		if forwarded := forwardedClientIP(r, opts); forwarded != "" {
			return forwarded
		}
	}

	return remoteIP.String()
}

func RateLimitIdentity(r *http.Request, apiKey string, opts ClientIPOptions) string {
	if key := strings.TrimSpace(apiKey); key != "" {
		return "key:" + rateLimitKeyHash(key)
	}

	if r == nil {
		return "ip:unknown"
	}

	ip := clientIP(r, opts)
	if ip == "" {
		return "ip:unknown"
	}

	return "ip:" + ip
}

func rateLimitKeyHash(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:8])
}

func NewFixedWindowRateLimiter(limit int, window time.Duration, opts FixedWindowOptions) *FixedWindowRateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	maxIdentities := opts.MaxIdentities
	if maxIdentities <= 0 {
		maxIdentities = defaultFixedWindowMaxIdentities
	}

	entryTTL := opts.EntryTTL
	if entryTTL <= 0 {
		entryTTL = defaultFixedWindowEntryTTL
	}

	if entryTTL < window {
		entryTTL = window
	}

	return &FixedWindowRateLimiter{
		limit:         limit,
		window:        window,
		maxIdentities: maxIdentities,
		entryTTL:      entryTTL,
		now:           now,
		entries:       make(map[string]*fixedWindowEntry),
	}
}

func (l *FixedWindowRateLimiter) Allow(identity string) bool {
	if l == nil {
		return true
	}

	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneExpired(now)

	entry, ok := l.entries[identity]
	if !ok {
		l.evictIfNeeded()

		entry = &fixedWindowEntry{identity: identity}
		entry.element = l.lru.PushBack(entry)
		l.entries[identity] = entry
	} else {
		l.lru.MoveToBack(entry.element)
	}

	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		entry.windowStart = now
		entry.count = 0
	}

	entry.lastSeen = now

	if entry.count >= l.limit {
		return false
	}

	entry.count++

	return true
}

func FixedWindowRateLimitMiddleware(cfg RateLimitMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Skip != nil && cfg.Skip(r) {
				next.ServeHTTP(w, r)

				return
			}

			identity := ""

			if cfg.Identity != nil {
				identity = cfg.Identity(r)
			}

			if cfg.Limiter.Allow(identity) {
				next.ServeHTTP(w, r)

				return
			}

			reject := cfg.Reject
			if reject == nil {
				reject = defaultRateLimitReject
			}

			reject(w, r, identity)
		})
	}
}

func (l *FixedWindowRateLimiter) pruneExpired(now time.Time) {
	if l.entryTTL <= 0 {
		return
	}

	for element := l.lru.Front(); element != nil; element = l.lru.Front() {
		entry := fixedWindowEntryForElement(element)
		if now.Sub(entry.lastSeen) < l.entryTTL {
			return
		}

		l.removeEntry(entry)
	}
}

func (l *FixedWindowRateLimiter) evictIfNeeded() {
	if l.maxIdentities <= 0 || len(l.entries) < l.maxIdentities {
		return
	}

	if element := l.lru.Front(); element != nil {
		l.removeEntry(fixedWindowEntryForElement(element))
	}
}

func fixedWindowEntryForElement(element *list.Element) *fixedWindowEntry {
	entry, ok := element.Value.(*fixedWindowEntry)
	if !ok {
		panic("httputil: invalid fixed-window LRU entry")
	}

	return entry
}

func (l *FixedWindowRateLimiter) removeEntry(entry *fixedWindowEntry) {
	delete(l.entries, entry.identity)
	l.lru.Remove(entry.element)
}

func forwardedClientIP(r *http.Request, opts ClientIPOptions) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return rightmostNonTrustedForwardedFor(xff, opts.TrustedProxies)
	}

	realIP, ok := parsePlainIPCandidate(r.Header.Get("X-Real-IP"))
	if !ok || isTrustedProxy(realIP, opts.TrustedProxies) {
		return ""
	}

	return realIP.String()
}

func isTrustedProxy(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func rightmostNonTrustedForwardedFor(headerValue string, trustedProxies []netip.Prefix) string {
	remaining := headerValue

	for {
		before, hop, found := strings.CutLast(remaining, ",")
		if !found {
			hop = remaining
		}

		candidate, ok := parsePlainIPCandidate(hop)
		if ok && !isTrustedProxy(candidate, trustedProxies) {
			return candidate.String()
		}

		if !found {
			return ""
		}

		remaining = before
	}
}

func parsePlainIPCandidate(value string) (netip.Addr, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return netip.Addr{}, false
	}

	ip := net.ParseIP(raw)
	if ip == nil {
		return netip.Addr{}, false
	}

	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	return netip.AddrFromSlice(ip)
}

func parseIPCandidate(value string) (netip.Addr, bool) {
	raw := strings.TrimSpace(strings.Trim(value, `"`))
	if raw == "" {
		return netip.Addr{}, false
	}

	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else {
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}

	return addr, true
}

func defaultRateLimitReject(w http.ResponseWriter, _ *http.Request, _ string) {
	http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
}
