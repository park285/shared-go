package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/idna"
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
	// ErrUnguardedTransport는 RequireGuardedDial이 dial 무보증 RoundTripper를 거부할 때의 오류다.
	ErrUnguardedTransport = errors.New("netguard: transport does not guard its dial path")
)

// stdlib net.Dialer의 saneMinimum과 같은 값으로, 후보가 많을 때 시도별 예산이 무의미하게 작아지는 것을 막는다.
const minDialAttemptTimeout = 2 * time.Second

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
	// RequireGuardedDial은 dial 무보증 RoundTripper를 GuardedClient에서 fail-closed로 거부한다.
	RequireGuardedDial bool

	// AllowedHosts를 ASCII 정규화한 사본이다. guard 생성 시 한 번 채우며, 비어 있으면
	// 요청 시점에 AllowedHosts로부터 다시 계산해 값으로 만든 Policy도 같은 결과를 낸다.
	normalizedAllowedHosts []string
}

// DialGuardedRoundTripper는 dial 시점 IP 정책을 보장할 수 없는 opaque RoundTripper가
// 스스로 그 정책을 적용했다고 선언하는 계약이다.
type DialGuardedRoundTripper interface {
	http.RoundTripper
	NetguardDialGuarded() bool
}

// RedirectConfig는 HTTP redirect 검증과 header 전달 정책이다.
type RedirectConfig struct {
	// Policy는 redirect target 검증에 사용할 정책이다.
	Policy Policy
	// MaxRedirects는 허용할 최대 redirect 수다.
	MaxRedirects int
	// DisableFollow는 redirect follow를 비활성화한다.
	DisableFollow bool
	// ForwardHeaders는 cross-origin(scheme·host·port) redirect에도 기존 header를 유지한다.
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

// dial 계층은 punycode, request 계층은 unicode host를 보므로 두 계층이 같은 형태를 비교하도록
// allowlist와 후보를 모두 ASCII로 맞춘다. 변환 실패 시 unicode 그대로 두어 fail-closed로 남긴다.
func normalizeHostASCII(host string) string {
	normalized := NormalizeHost(host)
	if isASCII(normalized) {
		return normalized
	}
	ascii, err := idna.Lookup.ToASCII(normalized)
	if err != nil {
		return normalized
	}
	return strings.ToLower(ascii)
}

func isASCII(value string) bool {
	for i := range len(value) {
		if value[i] >= 0x80 {
			return false
		}
	}
	return true
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
	if err := p.validateRequestTarget(target); err != nil {
		return err
	}

	host := normalizeHostASCII(target.Hostname())
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
	p = p.prepared()
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		resolved, err := p.resolveDialAddresses(ctx, address)
		if err != nil {
			return nil, err
		}
		var dialErrs []error
		for index, resolvedAddress := range resolved {
			attemptCtx, cancel, budgetErr := dialAttemptContext(ctx, len(resolved)-index)
			if budgetErr != nil {
				dialErrs = append(dialErrs, budgetErr)
				break
			}
			conn, err := base(attemptCtx, network, resolvedAddress)
			// net.Dialer/tls.Dialer 계약상 연결이 성립한 뒤의 context 취소는 conn에 영향이 없다.
			cancel()
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

// dialAttemptContext는 ctx에 남은 시간을 남은 후보 수로 나눠 첫 후보가 예산을 모두 쓰고
// 나머지 후보의 failover 기회를 없애는 것을 막는다. 마지막 후보는 남은 예산을 그대로 쓴다.
// ctx에 deadline이 없으면 base dialer의 자체 timeout이 그대로 적용된다.
func dialAttemptContext(ctx context.Context, remaining int) (context.Context, context.CancelFunc, error) {
	deadline, ok := ctx.Deadline()
	if !ok || remaining <= 1 {
		return ctx, func() {}, nil
	}
	budget := time.Until(deadline)
	if budget <= 0 {
		return nil, nil, fmt.Errorf("netguard: dial budget exhausted: %w", context.DeadlineExceeded)
	}
	timeout := budget / time.Duration(remaining)
	if timeout < minDialAttemptTimeout {
		timeout = min(budget, minDialAttemptTimeout)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	return attemptCtx, cancel, nil
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
	p = p.prepared()
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
// opaque RoundTripper 경로는 dial을 통제하지 못해 request 시점 resolve 결과만 검사하며,
// 반환 client의 Transport는 더 이상 *http.Transport로 단언되지 않는다.
func GuardedClient(client *http.Client, p Policy) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	p = p.prepared()
	cloned := *client
	if cloned.Transport == nil {
		cloned.Transport = guardedRoundTripper{base: GuardedTransport(nil, p), policy: p, dialGuarded: true}
		return &cloned
	}
	if transport, ok := cloned.Transport.(*http.Transport); ok {
		cloned.Transport = guardedRoundTripper{base: GuardedTransport(transport, p), policy: p, dialGuarded: true}
		return &cloned
	}

	declaredDialGuarded := false
	if capable, ok := cloned.Transport.(DialGuardedRoundTripper); ok {
		declaredDialGuarded = capable.NetguardDialGuarded()
	}
	if !declaredDialGuarded && p.RequireGuardedDial {
		cloned.Transport = unguardedRoundTripper{}
		return &cloned
	}
	// 선언은 RequireGuardedDial 요구를 충족할 뿐이며 검증 강도를 낮추지 않는다.
	cloned.Transport = guardedRoundTripper{base: cloned.Transport, policy: p}
	return &cloned
}

type guardedRoundTripper struct {
	base        http.RoundTripper
	policy      Policy
	dialGuarded bool
}

func (g guardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		closeRequestBody(req)
		return nil, errors.New("netguard: request URL is nil")
	}
	if g.dialGuarded {
		if err := g.policy.validateRequestTarget(req.URL); err != nil {
			closeRequestBody(req)
			return nil, err
		}
		return g.base.RoundTrip(req)
	}
	if err := g.policy.ValidateTarget(req.Context(), req.URL); err != nil {
		closeRequestBody(req)
		return nil, err
	}
	return g.base.RoundTrip(req)
}

// RoundTripper 계약상 거부 경로에서도 body를 닫아야 한다.
func closeRequestBody(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

func (g guardedRoundTripper) CloseIdleConnections() {
	if closer, ok := g.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type unguardedRoundTripper struct{}

func (unguardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	closeRequestBody(req)
	return nil, ErrUnguardedTransport
}

// RedirectPolicy는 redirect 대상도 Policy로 검증하는 CheckRedirect 함수다.
func RedirectPolicy(cfg RedirectConfig) func(req *http.Request, via []*http.Request) error {
	cfg.Policy = cfg.Policy.prepared()
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
		// net/http는 hop마다 최초 요청 header 사본을 복원하므로 직전 hop이 아니라 최초 요청 origin과
		// 비교해야 한다. 직전 hop과 비교하면 hop1에서 지운 header가 hop2에서 되살아난다.
		if !cfg.ForwardHeaders && len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			stripHeaders(req)
		}
		if cfg.CheckRedirect != nil {
			return cfg.CheckRedirect(req, via)
		}
		return nil
	}
}

func (p Policy) validateRequestTarget(target *url.URL) error {
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
	return p.validatePort(target)
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
	candidate := normalizeHostASCII(host)
	if len(p.AllowedHosts) > 0 && !slices.Contains(p.allowedHostsASCII(), candidate) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, candidate)
	}
	if p.AllowHost != nil && !p.AllowHost(candidate) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, candidate)
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
	if hostErr := p.validateHost(host); hostErr != nil {
		return nil, hostErr
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
	if !IsBlockedAddr(addr) {
		return true
	}
	for _, prefix := range p.AllowedIPPrefixes {
		if prefix.IsValid() && prefix.Contains(addr) {
			return true
		}
	}

	return false
}

// prepared는 요청마다 반복되던 allowlist 정규화를 guard 생성 시점으로 한 번만 옮긴다.
func (p Policy) prepared() Policy {
	if len(p.AllowedHosts) == 0 || p.normalizedAllowedHosts != nil {
		return p
	}
	p.normalizedAllowedHosts = normalizeHostListASCII(p.AllowedHosts)
	return p
}

func (p Policy) allowedHostsASCII() []string {
	if p.normalizedAllowedHosts != nil {
		return p.normalizedAllowedHosts
	}
	return normalizeHostListASCII(p.AllowedHosts)
}

func normalizeHostListASCII(hosts []string) []string {
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		normalized = append(normalized, normalizeHostASCII(host))
	}
	return normalized
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

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(a.Scheme), strings.TrimSpace(b.Scheme)) {
		return false
	}
	if normalizeHostASCII(a.Hostname()) != normalizeHostASCII(b.Hostname()) {
		return false
	}
	return effectivePort(a) == effectivePort(b)
}

func stripHeaders(req *http.Request) {
	for name := range req.Header {
		req.Header.Del(name)
	}
}
