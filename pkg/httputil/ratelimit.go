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

// ForwardedHeaderMode는 trusted proxy에서 전달된 client IP 선택 방식을 지정한다.
type ForwardedHeaderMode int

const (
	// ForwardedHeaderLeftmost는 X-Forwarded-For의 첫 유효 IP를 선택한다.
	ForwardedHeaderLeftmost ForwardedHeaderMode = iota
	// ForwardedHeaderRightmostNonTrusted는 오른쪽에서 첫 비신뢰 hop을 선택한다.
	ForwardedHeaderRightmostNonTrusted
)

// ClientIPOptions는 forwarded header 신뢰 경계를 지정한다.
type ClientIPOptions struct {
	TrustForwarded bool
	TrustedProxies []netip.Prefix
	ForwardedMode  ForwardedHeaderMode
}

// IdentityFunc는 요청에서 rate-limit identity를 만든다.
type IdentityFunc func(*http.Request) string

// RateLimitRejectFunc는 rate-limit 초과 응답을 쓴다.
type RateLimitRejectFunc func(http.ResponseWriter, *http.Request, string)

// FixedWindowOptions는 fixed-window limiter 선택 동작을 지정한다. MaxIdentities와 EntryTTL의 zero value는 안전 기본값을 사용하며, EntryTTL은 window보다 짧아지지 않는다.
type FixedWindowOptions struct {
	MaxIdentities int
	EntryTTL      time.Duration
	Now           func() time.Time
}

// FixedWindowRateLimiter는 identity별 fixed-window 요청 수를 제한한다.
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

// RateLimitMiddlewareConfig는 fixed-window HTTP middleware 설정이다.
type RateLimitMiddlewareConfig struct {
	Limiter  *FixedWindowRateLimiter
	Identity IdentityFunc
	Reject   RateLimitRejectFunc
	Skip     func(*http.Request) bool
}

// ParseTrustedProxies는 IP/CIDR 문자열을 trusted proxy prefix 목록으로 변환한다.
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
				return nil, fmt.Errorf("parse prefix: %w", err)
			}

			result = append(result, prefix.Masked())

			continue
		}

		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("parse addr: %w", err)
		}

		bits := 32

		if addr.Is6() {
			bits = 128
		}

		result = append(result, netip.PrefixFrom(addr, bits).Masked())
	}

	return result, nil
}

// ParseTrustedProxyCSV는 comma-separated trusted proxy CIDR/IP 목록을 변환한다.
func ParseTrustedProxyCSV(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	out, err := ParseTrustedProxies(strings.Split(raw, ","))
	if err != nil {
		return out, fmt.Errorf("parse trusted proxies: %w", err)
	}

	return out, nil
}

// ClientIP는 trusted proxy 경계 안에서만 forwarded header를 반영한 client IP를 반환한다.
func ClientIP(r *http.Request, opts ClientIPOptions) string {
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

	return remoteIP
}

// RateLimitIdentity는 API key가 있으면 key hash, 아니면 client IP 기반 identity를 반환한다.
func RateLimitIdentity(r *http.Request, apiKey string, opts ClientIPOptions) string {
	if key := strings.TrimSpace(apiKey); key != "" {
		return "key:" + RateLimitKeyHash(key)
	}

	if r == nil {
		return "ip:unknown"
	}

	clientIP := ClientIP(r, opts)
	if clientIP == "" {
		return "ip:unknown"
	}

	return "ip:" + clientIP
}

// RateLimitKeyHash는 rate-limit key/log용 짧은 SHA-256 해시를 반환한다.
func RateLimitKeyHash(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:8])
}

// NewFixedWindowRateLimiter는 fixed-window limiter를 생성한다.
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

// Allow는 identity가 현재 window에서 허용되는지 반환한다.
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

// FixedWindowRateLimitMiddleware는 fixed-window limiter를 net/http middleware로 감싼다.
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

// WriteRateLimitExceededJSON은 twentyq형 429 JSON 응답을 쓴다.
func WriteRateLimitExceededJSON(w http.ResponseWriter, _ *http.Request, _ string) {
	if err := WriteErrorJSON(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Too many requests"); err != nil {
		return
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
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		if opts.ForwardedMode == ForwardedHeaderRightmostNonTrusted {
			return rightmostNonTrustedForwardedFor(xff, opts.TrustedProxies)
		}

		return firstForwardedFor(xff)
	}

	parseRealIP := parseIPCandidate

	if opts.ForwardedMode == ForwardedHeaderRightmostNonTrusted {
		parseRealIP = parsePlainIPCandidate
	}

	realIP, ok := parseRealIP(r.Header.Get("X-Real-IP"))
	if !ok {
		return ""
	}

	if opts.ForwardedMode == ForwardedHeaderRightmostNonTrusted && isTrustedProxy(realIP, opts.TrustedProxies) {
		return ""
	}

	return realIP
}

func isTrustedProxy(ip string, trustedProxies []netip.Prefix) bool {
	if len(trustedProxies) == 0 {
		return false
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func firstForwardedFor(headerValue string) string {
	raw := strings.TrimSpace(headerValue)
	if raw == "" {
		return ""
	}

	for part := range strings.SplitSeq(raw, ",") {
		if ip, ok := parseIPCandidate(part); ok {
			return ip
		}
	}

	return ""
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
			return candidate
		}

		if !found {
			return ""
		}

		remaining = before
	}
}

func parsePlainIPCandidate(value string) (string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", false
	}

	ip := net.ParseIP(raw)
	if ip == nil {
		return "", false
	}

	return ip.String(), true
}

func parseIPCandidate(value string) (string, bool) {
	raw := strings.TrimSpace(strings.Trim(value, `"`))
	if raw == "" {
		return "", false
	}

	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	} else {
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", false
	}

	return addr.String(), true
}

func defaultRateLimitReject(w http.ResponseWriter, _ *http.Request, _ string) {
	http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
}
