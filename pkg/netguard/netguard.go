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
	ErrBlockedIP          = errors.New("netguard: target resolves to a blocked IP address")
	ErrHostNotAllowed     = errors.New("netguard: host not allowed")
	ErrTooManyRedirects   = errors.New("netguard: too many redirects")
	ErrUnsupportedScheme  = errors.New("netguard: unsupported URL scheme")
	ErrUnguardedTransport = errors.New("netguard: transport does not guard its dial path")
)

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

type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

type Policy struct {
	Resolver           Resolver
	Timeout            time.Duration
	AllowedIPPrefixes  []netip.Prefix
	AllowedHosts       []string
	AllowHost          func(string) bool
	AllowedPorts       []string
	Schemes            []string
	AllowUnguardedDial bool

	normalizedAllowedHosts []string
}

type dialGuardedTransport interface {
	http.RoundTripper
	NetguardDialGuarded() bool
}

type RedirectConfig struct {
	Policy         Policy
	MaxRedirects   int
	DisableFollow  bool
	ForwardHeaders bool
	CheckRedirect  func(req *http.Request, via []*http.Request) error
}

func IsBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}

	return isBlockedAddr(addr)
}

func isBlockedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}

	if addr.Zone() != "" {
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

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func normalizeHostASCII(host string) string {
	normalized := normalizeHost(host)
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

func (p Policy) ValidateTarget(ctx context.Context, target *url.URL) error {
	if err := p.validateRequestTarget(target); err != nil {
		return fmt.Errorf("netguard: validate request target: %w", err)
	}

	host := normalizeHostASCII(target.Hostname())

	ips, err := p.resolveHost(ctx, host)
	if err != nil {
		return fmt.Errorf("netguard: resolve host: %w", err)
	}

	for _, ip := range ips {
		if !p.allowsIP(ip) {
			return fmt.Errorf("%w: %s -> %s", ErrBlockedIP, host, ip)
		}
	}

	return nil
}

func (p Policy) resolveHost(ctx context.Context, host string) ([]net.IP, error) {
	host = normalizeHost(host)
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

func guardedDialContext(
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
			return nil, fmt.Errorf("netguard: resolve dial addresses: %w", err)
		}

		var dialErrs []error

		for index, resolvedAddress := range resolved {
			attemptCtx, cancel, budgetErr := dialAttemptContext(ctx, len(resolved)-index)
			if budgetErr != nil {
				dialErrs = append(dialErrs, budgetErr)
				break
			}

			conn, err := base(attemptCtx, network, resolvedAddress)

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
	base.DialTLS = nil //nolint:staticcheck // golangci-lint staticcheck도 같은 보안 우회 차단 예외를 인식시킨다.

	baseDialContext := base.DialContext
	if baseDialContext == nil {
		dialer := &net.Dialer{Timeout: p.Timeout}

		baseDialContext = dialer.DialContext
	}

	base.DialContext = guardedDialContext(baseDialContext, p)
	if base.DialTLSContext != nil {
		base.DialTLSContext = guardedDialContext(base.DialTLSContext, p)
	}

	return base
}

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

	if capable, ok := cloned.Transport.(dialGuardedTransport); ok {
		declaredDialGuarded = capable.NetguardDialGuarded()
	}

	if !declaredDialGuarded && !p.AllowUnguardedDial {
		cloned.Transport = unguardedRoundTripper{}
		return &cloned
	}

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

			return nil, fmt.Errorf("netguard: validate request target: %w", err)
		}

		out, err := g.base.RoundTrip(req)
		if err != nil {
			return nil, fmt.Errorf("netguard: round trip: %w", err)
		}

		return out, nil
	}

	if err := g.policy.ValidateTarget(req.Context(), req.URL); err != nil {
		closeRequestBody(req)

		return nil, err
	}

	out, err := g.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("netguard: round trip: %w", err)
	}

	return out, nil
}

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
		return fmt.Errorf("validate scheme: %w", err)
	}

	host := normalizeHost(target.Hostname())
	if host == "" {
		return errors.New("netguard: URL missing host")
	}

	if err := p.validateHost(host); err != nil {
		return fmt.Errorf("validate host: %w", err)
	}

	if err := p.validatePort(target); err != nil {
		return fmt.Errorf("validate port: %w", err)
	}

	return nil
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
	if err := p.validatePortString(effectivePort(target)); err != nil {
		return fmt.Errorf("validate port string: %w", err)
	}

	return nil
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
		return nil, fmt.Errorf("validate port string: %w", portErr)
	}

	if hostErr := p.validateHost(host); hostErr != nil {
		return nil, fmt.Errorf("validate host: %w", hostErr)
	}

	ips, err := p.resolveHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
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
	if !isBlockedAddr(addr) {
		return true
	}

	for _, prefix := range p.AllowedIPPrefixes {
		if prefix.IsValid() && prefix.Contains(addr) {
			return true
		}
	}

	return false
}

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
	clear(req.Header)
}
