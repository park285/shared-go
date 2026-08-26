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

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
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
		Resolver:     staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}},
		Timeout:      time.Second,
		AllowedHosts: []string{testExampleCom},
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

	policy := Policy{Resolver: staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}}}
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

	base := func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return nil, errors.New("stop after address capture")
	}
	policy := Policy{
		Resolver: staticResolver{
			testExampleCom: {net.ParseIP("93.184.216.34")},
			"internal.net": {net.ParseIP("127.0.0.1")},
		},
	}

	err := callGuardedDial(GuardedDialContext(base, policy), "example.com:443")
	if err == nil {
		t.Fatal("guarded dial error = nil, want base error")
	}

	if !slices.Equal(dialed, []string{testValue931842}) {
		t.Fatalf("dialed = %v, want resolved public address", dialed)
	}

	err = callGuardedDial(GuardedDialContext(base, policy), "internal.net:443")
	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("guarded dial error = %v, want ErrBlockedIP", err)
	}

	if !slices.Equal(dialed, []string{testValue931842}) {
		t.Fatalf("blocked target dialed addresses = %v, want unchanged", dialed)
	}
}

func TestGuardedDialContextFailsOverResolvedAddresses(t *testing.T) {
	t.Parallel()

	var (
		dialed []string
		peer   net.Conn
	)

	base := func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if address == testValue931842 {
			return nil, errors.New("first address failed")
		}

		conn, server := net.Pipe()

		peer = server

		return conn, nil
	}
	policy := Policy{
		Resolver: staticResolver{testExampleCom: {
			net.ParseIP("93.184.216.34"),
			net.ParseIP("93.184.216.35"),
		}},
	}

	conn, err := GuardedDialContext(base, policy)(t.Context(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("guarded dial error = %v", err)
	}

	defer testsupport.CloseNow(t, "conn.Close", conn.Close)
	defer testsupport.CloseNow(t, "peer.Close", peer.Close)

	if !slices.Equal(dialed, []string{testValue931842, "93.184.216.35:443"}) {
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
		Resolver:     staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}},
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
		Resolver:           staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}},
		AllowUnguardedDial: true,
	}

	req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://internal.test/secret", http.NoBody)
	if reqErr != nil {
		t.Fatalf("NewRequestWithContext() error = %v", reqErr)
	}

	resp, err := GuardedClient(client, policy).Do(req)
	if err == nil {
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Errorf("Body.Close() error = %v", closeErr)
			}
		}()
	}

	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrBlockedIP", err)
	}

	if called {
		t.Fatal("inner RoundTripper was called for blocked target")
	}
}

func TestGuardedClientRejectsDisallowedHostBeforeDial(t *testing.T) {
	t.Parallel()

	dialed := 0
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("must not dial")
		},
	}
	policy := Policy{
		Resolver:     staticResolver{"blocked.test": {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{testAllowedTest},
	}

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://blocked.test/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := GuardedClient(&http.Client{Transport: transport}, policy).Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrHostNotAllowed", err)
	}

	if dialed != 0 {
		t.Fatalf("dial attempts = %d, want 0 (host must be rejected before dial)", dialed)
	}
}

func TestGuardedClientRejectsDisallowedSchemeAndPortBeforeDial(t *testing.T) {
	t.Parallel()

	dialed := 0
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("must not dial")
		},
	}
	policy := Policy{
		Resolver:     staticResolver{testAllowedTest: {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{testAllowedTest},
		AllowedPorts: []string{"443"},
		Schemes:      []string{"https"},
	}
	client := GuardedClient(&http.Client{Transport: transport}, policy)

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://allowed.test:8443/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if err == nil || !strings.Contains(err.Error(), `port "8443" is not allowed`) {
		t.Fatalf("GuardedClient().Get() port error = %v, want disallowed port", err)
	}

	respReq, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://allowed.test/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err = client.Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("GuardedClient().Get() scheme error = %v, want ErrUnsupportedScheme", err)
	}

	if dialed != 0 {
		t.Fatalf("dial attempts = %d, want 0", dialed)
	}
}

func TestGuardedTransportRejectsDisallowedHostAtDial(t *testing.T) {
	t.Parallel()

	dialed := 0
	base := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("must not dial")
		},
	}
	policy := Policy{
		Resolver:     staticResolver{"blocked.test": {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{testAllowedTest},
	}

	guarded := GuardedTransport(base, policy)
	_, err := guarded.DialContext(t.Context(), "tcp", "blocked.test:443")

	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("GuardedTransport().DialContext() error = %v, want ErrHostNotAllowed", err)
	}

	if dialed != 0 {
		t.Fatalf("dial attempts = %d, want 0", dialed)
	}
}

func TestGuardedClientDialsPinnedAddressUnderDNSRebinding(t *testing.T) {
	t.Parallel()

	resolver := &sequenceResolver{answers: [][]net.IP{
		{net.ParseIP("93.184.216.34")},
		{net.ParseIP("127.0.0.1")},
	}}

	var dialed []string

	transport := &http.Transport{
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			return nil, errors.New("stop after address capture")
		},
	}
	policy := Policy{Resolver: resolver, AllowedHosts: []string{"rebind.test"}}

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://rebind.test/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := GuardedClient(&http.Client{Transport: transport}, policy).Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if err == nil {
		t.Fatal("GuardedClient().Get() error = nil, want captured dial error")
	}

	if !slices.Equal(dialed, []string{testValue931842}) {
		t.Fatalf("dialed = %v, want only the validated literal address", dialed)
	}

	if got := resolver.Calls(); got != 1 {
		t.Fatalf("resolver calls = %d, want 1 (dial-time answer must be the one dialed)", got)
	}
}

func TestGuardedClientRejectsRebindingToBlockedAnswer(t *testing.T) {
	t.Parallel()

	resolver := &sequenceResolver{answers: [][]net.IP{{net.ParseIP("127.0.0.1")}}}
	dialed := 0
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("must not dial")
		},
	}
	policy := Policy{Resolver: resolver, AllowedHosts: []string{"rebind.test"}}

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://rebind.test/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := GuardedClient(&http.Client{Transport: transport}, policy).Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, ErrBlockedIP) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrBlockedIP", err)
	}

	if dialed != 0 {
		t.Fatalf("dial attempts = %d, want 0", dialed)
	}
}

func TestGuardedClientRejectsOpaqueTransportByDefault(t *testing.T) {
	t.Parallel()

	called := false
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("must not call")
		}),
	}
	policy := Policy{Resolver: staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}}}

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := GuardedClient(client, policy).Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, ErrUnguardedTransport) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrUnguardedTransport", err)
	}

	if called {
		t.Fatal("opaque RoundTripper was called without AllowUnguardedDial")
	}
}

func TestGuardedClientAcceptsDeclaredDialGuardedTransport(t *testing.T) {
	t.Parallel()

	stub := &dialGuardedRoundTripper{}
	policy := Policy{
		Resolver:     staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{testExampleCom},
	}
	client := GuardedClient(&http.Client{Transport: stub}, policy)

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(respReq)
	if err != nil {
		t.Fatalf("GuardedClient().Get() error = %v", err)
	}

	_ = resp.Body.Close()

	if stub.calls != 1 {
		t.Fatalf("declared dial-guarded RoundTripper calls = %d, want 1", stub.calls)
	}

	respReq, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "https://other.example/resource", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err = client.Do(respReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("GuardedClient().Get() error = %v, want ErrHostNotAllowed", err)
	}

	if stub.calls != 1 {
		t.Fatalf("declared dial-guarded RoundTripper calls = %d, want 1 (host must be rejected first)", stub.calls)
	}
}
