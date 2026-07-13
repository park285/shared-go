package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrBlockedIP는 대상이 차단된 IP 주소로 확인될 때의 오류다.
	ErrBlockedIP = errors.New("netguard: target resolves to a blocked IP address")
	// ErrHostNotAllowed는 대상 host가 allowlist 정책을 통과하지 못할 때의 오류다.
	ErrHostNotAllowed = errors.New("netguard: host not allowed")
	// ErrTooManyRedirects는 redirect 횟수가 정책 한도를 넘을 때의 오류다.
	ErrTooManyRedirects = errors.New("netguard: too many redirects")
	// ErrUnsupportedScheme는 URL scheme이 정책에서 허용되지 않을 때의 오류다.
	ErrUnsupportedScheme = errors.New("netguard: unsupported URL scheme")
)

var blockedAddressPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// Resolver는 host를 IP 주소 목록으로 확인한다.
type Resolver interface {
	// LookupIP는 host의 IP 주소 목록을 반환한다.
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Policy는 외부 네트워크 대상 검증 규칙이다.
type Policy struct {
	// Resolver는 host lookup에 사용할 resolver다.
	Resolver Resolver
	// Timeout은 lookup과 기본 dial timeout이다.
	Timeout time.Duration
	// AllowPrivateNetworks는 private 및 특수 목적 IP 대역을 모두 허용하는 하위 호환 옵션이다.
	//
	// Deprecated: 필요한 대역만 AllowedIPPrefixes로 명시하십시오.
	AllowPrivateNetworks bool
	// AllowedIPPrefixes는 기본 차단 대역 중 의도적으로 허용할 IP prefix 목록이다.
	AllowedIPPrefixes []netip.Prefix
	// AllowedHosts는 허용할 host allowlist다.
	AllowedHosts []string
	// AllowHost는 host별 추가 허용 여부를 판단한다.
	AllowHost func(string) bool
	// AllowedPorts는 허용할 destination port 목록이다.
	AllowedPorts []string
	// Schemes는 허용할 URL scheme 목록이다.
	Schemes []string
}

// RedirectConfig는 HTTP redirect 검증과 header 전달 정책이다.
type RedirectConfig struct {
	// Policy는 redirect target 검증에 사용할 정책이다.
	Policy Policy
	// MaxRedirects는 허용할 최대 redirect 수다.
	MaxRedirects int
	// DisableFollow는 redirect follow를 비활성화한다.
	DisableFollow bool
	// ForwardHeaders는 cross-host redirect에도 기존 header를 유지한다.
	ForwardHeaders bool
	// CheckRedirect는 정책 검증 뒤 실행할 추가 redirect hook이다.
	CheckRedirect func(req *http.Request, via []*http.Request) error
}

// IsBlockedIP는 IP가 private 또는 특수 목적 대역인지 검사한다.
func IsBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return IsBlockedAddr(addr)
}

// IsBlockedAddr는 주소가 private 또는 특수 목적 대역인지 검사한다.
func IsBlockedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	for index := range blockedAddressPrefixes {
		if blockedAddressPrefixes[index].Contains(addr) {
			return true
		}
	}

	return !addr.IsGlobalUnicast()
}

// NormalizeHost는 비교용 host 문자열을 정규화한다.
func NormalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// ValidateURL은 URL 문자열을 파싱하고 정책에 맞는지 검증한다.
func (p Policy) ValidateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("netguard: empty URL")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("netguard: parse URL: %w", err)
	}
	if err := p.ValidateTarget(ctx, parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// ValidateTarget은 파싱된 URL 대상이 정책에 맞는지 검증한다.
func (p Policy) ValidateTarget(ctx context.Context, target *url.URL) error {
	if target == nil {
		return errors.New("netguard: target URL is nil")
	}
	if err := p.validateScheme(target.Scheme); err != nil {
		return err
	}

	host := NormalizeHost(target.Hostname())
	if host == "" {
		return errors.New("netguard: URL missing host")
	}
	if err := p.validateHost(host); err != nil {
		return err
	}
	if err := p.validatePort(target); err != nil {
		return err
	}

	ips, err := p.ResolveHost(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !p.allowsIP(ip) {
			return fmt.Errorf("%w: %s -> %s", ErrBlockedIP, host, ip)
		}
	}
	return nil
}

// ResolveHost는 정책 resolver로 host의 IP 주소 목록을 반환한다.
func (p Policy) ResolveHost(ctx context.Context, host string) ([]net.IP, error) {
	host = NormalizeHost(host)
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	lookupCtx := ctx
	cancel := func() {}
	if p.Timeout > 0 {
		lookupCtx, cancel = context.WithTimeout(ctx, p.Timeout)
	}
	defer cancel()

	ips, err := resolver.LookupIP(lookupCtx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("netguard: resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("netguard: resolve host %q: no addresses", host)
	}
	return ips, nil
}

// GuardedDialContext는 dial 전에 host, port, IP 정책을 검증하는 dial 함수를 만든다.
func GuardedDialContext(
	base func(context.Context, string, string) (net.Conn, error),
	p Policy,
) func(context.Context, string, string) (net.Conn, error) {
	if base == nil {
		dialer := &net.Dialer{Timeout: p.Timeout}
		base = dialer.DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		resolved, err := p.resolveDialAddresses(ctx, address)
		if err != nil {
			return nil, err
		}
		var dialErrs []error
		for _, resolvedAddress := range resolved {
			conn, err := base(ctx, network, resolvedAddress)
			if err == nil {
				return conn, nil
			}
			dialErrs = append(dialErrs, fmt.Errorf("netguard: dial %s: %w", resolvedAddress, err))
		}
		if len(dialErrs) == 0 {
			return nil, errors.New("netguard: no resolved dial addresses")
		}
		return nil, errors.Join(dialErrs...)
	}
}

// GuardedTransport는 http.Transport의 dial 경로에 Policy 검증을 적용한다.
func GuardedTransport(base *http.Transport, p Policy) *http.Transport {
	if base == nil {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			base = defaultTransport.Clone()
		} else {
			base = &http.Transport{}
		}
	} else {
		base = base.Clone()
	}
	base.Proxy = nil
	//lint:ignore SA1019 deprecated DialTLS를 비워 소비자 제공 unguarded DialTLS 우회를 막는다.
	base.DialTLS = nil //nolint:staticcheck // golangci-lint staticcheck도 같은 보안 우회 차단 예외를 인식시킨다.

	baseDialContext := base.DialContext
	if baseDialContext == nil {
		dialer := &net.Dialer{Timeout: p.Timeout}
		baseDialContext = dialer.DialContext
	}
	base.DialContext = GuardedDialContext(baseDialContext, p)
	if base.DialTLSContext != nil {
		base.DialTLSContext = GuardedDialContext(base.DialTLSContext, p)
	}
	return base
}

// GuardedClient는 http.Client transport에 Policy 검증을 적용한 복사본을 반환한다.
func GuardedClient(client *http.Client, p Policy) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	if cloned.Transport == nil {
		cloned.Transport = GuardedTransport(nil, p)
		return &cloned
	}
	if transport, ok := cloned.Transport.(*http.Transport); ok {
		cloned.Transport = GuardedTransport(transport, p)
	} else {
		cloned.Transport = guardedRoundTripper{base: cloned.Transport, policy: p}
	}
	return &cloned
}

type guardedRoundTripper struct {
	base   http.RoundTripper
	policy Policy
}

func (g guardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("netguard: request URL is nil")
	}
	if err := g.policy.ValidateTarget(req.Context(), req.URL); err != nil {
		return nil, err
	}
	return g.base.RoundTrip(req)
}

// RedirectPolicy는 redirect 대상도 Policy로 검증하는 CheckRedirect 함수다.
func RedirectPolicy(cfg RedirectConfig) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if cfg.DisableFollow {
			return http.ErrUseLastResponse
		}
		maxRedirects := cfg.MaxRedirects
		if maxRedirects <= 0 {
			maxRedirects = 10
		}
		if len(via) > maxRedirects {
			return ErrTooManyRedirects
		}
		if req == nil || req.URL == nil {
			return errors.New("netguard: redirect target is nil")
		}
		if err := cfg.Policy.ValidateTarget(req.Context(), req.URL); err != nil {
			return err
		}
		if !cfg.ForwardHeaders && len(via) > 0 && !sameHost(via[len(via)-1].URL, req.URL) {
			stripHeaders(req)
		}
		if cfg.CheckRedirect != nil {
			return cfg.CheckRedirect(req, via)
		}
		return nil
	}
}

func (p Policy) validateScheme(scheme string) error {
	normalized := strings.ToLower(strings.TrimSpace(scheme))
	if normalized == "" {
		return fmt.Errorf("%w: empty", ErrUnsupportedScheme)
	}

	schemes := p.Schemes
	if len(schemes) == 0 {
		schemes = []string{"http", "https"}
	}
	for _, allowed := range schemes {
		if normalized == strings.ToLower(strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedScheme, scheme)
}

func (p Policy) validateHost(host string) error {
	if len(p.AllowedHosts) > 0 && !hostInList(host, p.AllowedHosts) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
	}
	if p.AllowHost != nil && !p.AllowHost(host) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
	}
	return nil
}

func (p Policy) validatePort(target *url.URL) error {
	return p.validatePortString(effectivePort(target))
}

func (p Policy) validatePortString(port string) error {
	if len(p.AllowedPorts) == 0 {
		return nil
	}
	for _, allowed := range p.AllowedPorts {
		if port == strings.TrimSpace(allowed) {
			return nil
		}
	}
	return fmt.Errorf("netguard: port %q is not allowed", port)
}

func (p Policy) resolveDialAddresses(ctx context.Context, address string) ([]string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("netguard: split dial address: %w", err)
	}
	if portErr := p.validatePortString(port); portErr != nil {
		return nil, portErr
	}

	ips, err := p.ResolveHost(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !p.allowsIP(ip) {
			return nil, fmt.Errorf("%w: %s -> %s", ErrBlockedIP, host, ip)
		}
	}

	resolved := make([]string, 0, len(ips))
	for _, ip := range ips {
		resolved = append(resolved, net.JoinHostPort(ip.String(), port))
	}
	return resolved, nil
}

func (p Policy) allowsIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !IsBlockedAddr(addr) || p.AllowPrivateNetworks {
		return true
	}
	for _, prefix := range p.AllowedIPPrefixes {
		if prefix.IsValid() && prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func hostInList(host string, allowed []string) bool {
	host = NormalizeHost(host)
	for _, candidate := range allowed {
		if host == NormalizeHost(candidate) {
			return true
		}
	}
	return false
}

func effectivePort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func sameHost(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return NormalizeHost(a.Hostname()) == NormalizeHost(b.Hostname())
}

func stripHeaders(req *http.Request) {
	for name := range req.Header {
		req.Header.Del(name)
	}
}
