package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{name: "nil", ip: nil, want: true},
		{name: "invalid", ip: net.IP{1, 2, 3}, want: true},
		{name: "unspecified", ip: net.ParseIP("0.0.0.0"), want: true},
		{name: "loopback", ip: net.ParseIP("127.0.0.1"), want: true},
		{name: "private", ip: net.ParseIP("10.0.0.1"), want: true},
		{name: "carrier grade NAT", ip: net.ParseIP("100.64.0.1"), want: true},
		{name: "link local metadata", ip: net.ParseIP("169.254.169.254"), want: true},
		{name: "mapped link local metadata", ip: net.ParseIP("::ffff:169.254.169.254"), want: true},
		{name: "documentation v4", ip: net.ParseIP("192.0.2.1"), want: true},
		{name: "mapped documentation v4", ip: net.ParseIP("::ffff:192.0.2.1"), want: true},
		{name: "benchmark v4", ip: net.ParseIP("198.18.0.1"), want: true},
		{name: "reserved v4", ip: net.ParseIP("240.0.0.1"), want: true},
		{name: "limited broadcast", ip: net.ParseIP("255.255.255.255"), want: true},
		{name: "multicast", ip: net.ParseIP("224.0.0.1"), want: true},
		{name: "ipv6 loopback", ip: net.ParseIP("::1"), want: true},
		{name: "NAT64 well known metadata", ip: net.ParseIP("64:ff9b::a9fe:a9fe"), want: true},
		{name: "NAT64 local use", ip: net.ParseIP("64:ff9b:1::1"), want: true},
		{name: "ipv6 discard only", ip: net.ParseIP("100::1"), want: true},
		{name: "ipv6 dummy", ip: net.ParseIP("100:0:0:1::1"), want: true},
		{name: "ipv6 benchmark", ip: net.ParseIP("2001:2::1"), want: true},
		{name: "ipv6 documentation", ip: net.ParseIP("2001:db8::1"), want: true},
		{name: "ipv6 documentation second block", ip: net.ParseIP("3fff::1"), want: true},
		{name: "ipv6 segment routing SID", ip: net.ParseIP("5f00::1"), want: true},
		{name: "ipv6 link local", ip: net.ParseIP("fe80::1"), want: true},
		{name: "ipv6 multicast", ip: net.ParseIP("ff02::1"), want: true},
		{name: "public dns", ip: net.ParseIP("8.8.8.8"), want: false},
		{name: "public example", ip: net.ParseIP("93.184.216.34"), want: false},
		{name: "public ipv6", ip: net.ParseIP("2001:4860:4860::8888"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsBlockedIP(tt.ip); got != tt.want {
				t.Fatalf("IsBlockedIP(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestPolicyValidateTarget(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver:     staticResolver{"example.com": {net.ParseIP("93.184.216.34")}},
		Timeout:      time.Second,
		AllowedHosts: []string{"example.com"},
		AllowedPorts: []string{"443"},
	}
	if _, err := policy.ValidateURL(t.Context(), "https://example.com/path"); err != nil {
		t.Fatalf("ValidateURL() error = %v", err)
	}

	tests := []struct {
		name    string
		rawURL  string
		wantErr error
	}{
		{name: "host", rawURL: "https://other.example/path", wantErr: ErrHostNotAllowed},
		{name: "port", rawURL: "https://example.com:444/path"},
		{name: "scheme", rawURL: "file://example.com/path", wantErr: ErrUnsupportedScheme},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := policy.ValidateURL(t.Context(), tt.rawURL)
			if err == nil {
				t.Fatal("ValidateURL() error = nil, want error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateURL() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPolicyValidateTargetRejectsResolvedPrivateIP(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}},
	}
	_, err := policy.ValidateURL(t.Context(), "https://internal.test/secret")
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("ValidateURL() error = %v, want ErrBlockedIP", err)
	}
}

func TestPolicyValidateTargetRejectsAnyBlockedDNSAnswer(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{"mixed.test": {
			net.ParseIP("8.8.8.8"),
			net.ParseIP("100.64.0.1"),
		}},
	}
	_, err := policy.ValidateURL(t.Context(), "https://mixed.test/resource")
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("ValidateURL() error = %v, want ErrBlockedIP", err)
	}
}

func TestPolicyValidateTargetAllowsExplicitPrivatePrefix(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver:          staticResolver{"overlay.test": {net.ParseIP("100.64.0.1")}},
		AllowedIPPrefixes: []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")},
	}
	if _, err := policy.ValidateURL(t.Context(), "https://overlay.test/resource"); err != nil {
		t.Fatalf("ValidateURL() error = %v, want explicit prefix allowed", err)
	}
}

func TestGuardedDialContextResolvesAndBlocksPrivateTargets(t *testing.T) {
	t.Parallel()

	var dialed []string
	base := func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return nil, errors.New("stop after address capture")
	}
	policy := Policy{
		Resolver: staticResolver{
			"example.com":  {net.ParseIP("93.184.216.34")},
			"internal.net": {net.ParseIP("127.0.0.1")},
		},
	}

	err := callGuardedDial(GuardedDialContext(base, policy), "example.com:443")
	if err == nil {
		t.Fatal("guarded dial error = nil, want base error")
	}
	if !slices.Equal(dialed, []string{"93.184.216.34:443"}) {
		t.Fatalf("dialed = %v, want resolved public address", dialed)
	}

	err = callGuardedDial(GuardedDialContext(base, policy), "internal.net:443")
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("guarded dial error = %v, want ErrBlockedIP", err)
	}
	if !slices.Equal(dialed, []string{"93.184.216.34:443"}) {
		t.Fatalf("blocked target dialed addresses = %v, want unchanged", dialed)
	}
}

func TestGuardedDialContextFailsOverResolvedAddresses(t *testing.T) {
	t.Parallel()

	var dialed []string
	var peer net.Conn
	base := func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if address == "93.184.216.34:443" {
			return nil, errors.New("first address failed")
		}
		conn, server := net.Pipe()
		peer = server
		return conn, nil
	}
	policy := Policy{
		Resolver: staticResolver{"example.com": {
			net.ParseIP("93.184.216.34"),
			net.ParseIP("93.184.216.35"),
		}},
	}

	conn, err := GuardedDialContext(base, policy)(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("guarded dial error = %v", err)
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = peer.Close() }()
	if !slices.Equal(dialed, []string{"93.184.216.34:443", "93.184.216.35:443"}) {
		t.Fatalf("dialed = %v, want failover across resolved addresses", dialed)
	}
}

func TestGuardedDialContextRejectsDisallowedPort(t *testing.T) {
	t.Parallel()

	called := false
	base := func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("must not dial")
	}
	policy := Policy{
		Resolver:     staticResolver{"example.com": {net.ParseIP("93.184.216.34")}},
		AllowedPorts: []string{"443"},
	}

	err := callGuardedDial(GuardedDialContext(base, policy), "example.com:80")
	if err == nil {
		t.Fatal("guarded dial error = nil, want disallowed port")
	}
	if !strings.Contains(err.Error(), `port "80" is not allowed`) {
		t.Fatalf("guarded dial error = %v, want disallowed port", err)
	}
	if called {
		t.Fatal("base dial was called for disallowed port")
	}
}

func TestGuardedTransportDisablesProxy(t *testing.T) {
	t.Parallel()

	base := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) {
			return &url.URL{Scheme: "http", Host: "proxy.test:8080"}, nil
		},
	}

	guarded := GuardedTransport(base, Policy{})
	if guarded.Proxy != nil {
		t.Fatal("GuardedTransport().Proxy is non-nil, want nil")
	}
	if base.Proxy == nil {
		t.Fatal("GuardedTransport mutated base Proxy")
	}
}

func TestGuardedClientWrapsNonTransportRoundTripper(t *testing.T) {
	t.Parallel()

	called := false
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("must not call")
		}),
	}
	policy := Policy{
		Resolver: staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}},
	}

	_, err := GuardedClient(client, policy).Get("https://internal.test/secret")
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrBlockedIP", err)
	}
	if called {
		t.Fatal("inner RoundTripper was called for blocked target")
	}
}

func TestRedirectPolicyValidatesTargetAndStripsCrossHostHeaders(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			"example.com": {net.ParseIP("93.184.216.34")},
			"other.com":   {net.ParseIP("93.184.216.35")},
		},
	}
	req := &http.Request{
		URL:    mustURL(t, "https://other.com/final"),
		Header: http.Header{"Authorization": []string{"Bearer token"}},
	}
	via := []*http.Request{{URL: mustURL(t, "https://example.com/start")}}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 2})(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization after cross-host redirect = %q, want empty", got)
	}

	blockedReq := &http.Request{URL: mustURL(t, "https://127.0.0.1/private")}
	if err := RedirectPolicy(RedirectConfig{Policy: policy})(blockedReq, via); !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("RedirectPolicy() private error = %v, want ErrBlockedIP", err)
	}

	cgnatReq := &http.Request{URL: mustURL(t, "https://100.64.0.1/private")}
	if err := RedirectPolicy(RedirectConfig{Policy: policy})(cgnatReq, via); !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("RedirectPolicy() CGNAT error = %v, want ErrBlockedIP", err)
	}
}

func TestRedirectPolicyLimitAndDisableFollow(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: mustURL(t, "https://example.com/final")}
	via := []*http.Request{
		{URL: mustURL(t, "https://example.com/one")},
		{URL: mustURL(t, "https://example.com/two")},
	}
	policy := Policy{Resolver: staticResolver{"example.com": {net.ParseIP("93.184.216.34")}}}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 1})(req, via); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("RedirectPolicy() error = %v, want ErrTooManyRedirects", err)
	}
	defaultVia := make([]*http.Request, 11)
	for i := range defaultVia {
		defaultVia[i] = &http.Request{URL: mustURL(t, "https://example.com/hop")}
	}
	if err := RedirectPolicy(RedirectConfig{Policy: policy})(req, defaultVia); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("RedirectPolicy(default) error = %v, want ErrTooManyRedirects", err)
	}
	if err := RedirectPolicy(RedirectConfig{DisableFollow: true})(req, via); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("RedirectPolicy(disable) error = %v, want http.ErrUseLastResponse", err)
	}
}

type staticResolver map[string][]net.IP

func (r staticResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	ips, ok := r[NormalizeHost(host)]
	if !ok {
		return nil, &net.DNSError{Name: host, Err: "not found"}
	}
	return ips, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func callGuardedDial(dial func(context.Context, string, string) (net.Conn, error), address string) error {
	_, err := dial(context.Background(), "tcp", address)
	return err
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return parsed
}
