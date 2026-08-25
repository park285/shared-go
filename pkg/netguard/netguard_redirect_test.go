package netguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"

	"strings"
	"sync"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestGuardedClientRequireGuardedDialNeverWeakensValidation(t *testing.T) {
	t.Parallel()

	for _, requireGuardedDial := range []bool{false, true} {
		t.Run("require_guarded_dial", func(t *testing.T) {
			t.Parallel()

			stub := &dialGuardedRoundTripper{}
			policy := Policy{
				Resolver:           staticResolver{"internal.test": {net.ParseIP("127.0.0.1")}},
				RequireGuardedDial: requireGuardedDial,
			}

			respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://internal.test/secret", http.NoBody)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}

			resp, err := GuardedClient(&http.Client{Transport: stub}, policy).Do(respReq)
			if resp != nil {
				_ = resp.Body.Close()
			}

			if !errors.Is(err, ErrBlockedIP) {
				t.Fatalf("RequireGuardedDial=%v error = %v, want ErrBlockedIP", requireGuardedDial, err)
			}

			if stub.calls != 0 {
				t.Fatalf("RequireGuardedDial=%v declared RoundTripper calls = %d, want 0", requireGuardedDial, stub.calls)
			}
		})
	}
}

func TestGuardedRoundTripperClosesRequestBodyOnReject(t *testing.T) {
	t.Parallel()

	blockedPolicy := Policy{
		Resolver:     staticResolver{"blocked.test": {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{testAllowedTest},
	}
	tests := []struct {
		name      string
		transport http.RoundTripper
		policy    Policy
		wantErr   error
	}{
		{
			name:      "standard transport",
			transport: &http.Transport{},
			policy:    blockedPolicy,
			wantErr:   ErrHostNotAllowed,
		},
		{
			name: "opaque transport",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("must not call")
			}),
			policy:  blockedPolicy,
			wantErr: ErrHostNotAllowed,
		},
		{
			name: "unguarded transport",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("must not call")
			}),
			policy:  Policy{RequireGuardedDial: true},
			wantErr: ErrUnguardedTransport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := &trackedBody{}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://blocked.test/resource", body)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}

			guarded := GuardedClient(&http.Client{Transport: tt.transport}, tt.policy)

			resp, err := guarded.Transport.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RoundTrip() error = %v, want %v", err, tt.wantErr)
			}

			if body.Closed() != 1 {
				t.Fatalf("request body closes = %d, want 1 (RoundTripper must close body on reject)", body.Closed())
			}
		})
	}
}

func TestPolicyMatchesIDNHostInEitherAllowlistForm(t *testing.T) {
	t.Parallel()

	const (
		unicodeHost  = "bücher.example"
		punycodeHost = "xn--bcher-kva.example"
	)

	resolver := staticResolver{punycodeHost: {net.ParseIP("93.184.216.34")}}

	tests := []struct {
		name         string
		allowedHosts []string
	}{
		{name: "unicode allowlist", allowedHosts: []string{unicodeHost}},
		{name: "punycode allowlist", allowedHosts: []string{punycodeHost}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := Policy{Resolver: resolver, AllowedHosts: tt.allowedHosts}

			if _, err := policy.ValidateURL(t.Context(), "https://"+unicodeHost+"/path"); err != nil {
				t.Fatalf("ValidateURL(unicode request) error = %v, want allowed", err)
			}

			if _, err := policy.ValidateURL(t.Context(), "https://"+punycodeHost+"/path"); err != nil {
				t.Fatalf("ValidateURL(punycode request) error = %v, want allowed", err)
			}

			dialed := 0
			base := func(_ context.Context, _, address string) (net.Conn, error) {
				dialed++

				if address != testValue931842 {
					t.Errorf("dialed address = %q, want resolved literal", address)
				}

				return nil, errors.New("stop after address capture")
			}

			if err := callGuardedDial(GuardedDialContext(base, policy), punycodeHost+":443"); err == nil {
				t.Fatal("guarded dial error = nil, want captured dial error")
			}

			if dialed != 1 {
				t.Fatalf("punycode dial attempts = %d, want 1 (dial layer must match allowlist)", dialed)
			}
		})
	}
}

func TestPolicyRejectsDisallowedIDNHost(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver:     staticResolver{"xn--bcher-kva.example": {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{"allowed.example"},
	}

	if _, err := policy.ValidateURL(t.Context(), "https://bücher.example/path"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("ValidateURL(unicode) error = %v, want ErrHostNotAllowed", err)
	}

	if err := callGuardedDial(GuardedDialContext(nil, policy), "xn--bcher-kva.example:443"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("guarded dial error = %v, want ErrHostNotAllowed", err)
	}
}

func TestRedirectPolicyKeepsHeadersAcrossEquivalentIDNForms(t *testing.T) {
	t.Parallel()

	const (
		unicodeHost  = "bücher.example"
		punycodeHost = "xn--bcher-kva.example"
	)

	policy := Policy{
		Resolver:     staticResolver{punycodeHost: {net.ParseIP("93.184.216.34")}},
		AllowedHosts: []string{unicodeHost},
	}
	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "unicode to punycode", from: "https://" + unicodeHost + "/start", to: "https://" + punycodeHost + "/next"},
		{name: "punycode to unicode", from: "https://" + punycodeHost + "/start", to: "https://" + unicodeHost + "/next"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			req := &http.Request{
				URL: mustURL(t, testCase.to),
				Header: http.Header{
					testAuthorization: []string{testBearerToken},
					"Cookie":          []string{"session=abc"},
				},
			}
			via := []*http.Request{{URL: mustURL(t, testCase.from)}}

			if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 3})(req, via); err != nil {
				t.Fatalf("RedirectPolicy() error = %v", err)
			}

			if got := req.Header.Get(testAuthorization); got != testBearerToken {
				t.Fatalf("Authorization = %q, want preserved", got)
			}

			if got := req.Header.Get("Cookie"); got != "session=abc" {
				t.Fatalf("Cookie = %q, want preserved", got)
			}
		})
	}
}

func TestRedirectPolicyStripsHeadersRestoredOnLaterHop(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			"start.test": {net.ParseIP("93.184.216.34")},
			"relay.test": {net.ParseIP("93.184.216.35")},
		},
	}
	// net/http가 hop마다 최초 header를 복원하므로 hop2 요청도 credential을 들고 들어온다.
	req := &http.Request{
		URL:    mustURL(t, "https://relay.test/3"),
		Header: http.Header{testAuthorization: []string{testBearerToken}},
	}
	via := []*http.Request{
		{URL: mustURL(t, "https://start.test/1")},
		{URL: mustURL(t, "https://relay.test/2")},
	}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 5})(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}

	if got := req.Header.Get(testAuthorization); got != "" {
		t.Fatalf("multi-hop redirect Authorization = %q, want empty (origin is start.test)", got)
	}
}

func TestRedirectPolicyKeepsHeadersReturningToOriginalOrigin(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			"start.test": {net.ParseIP("93.184.216.34")},
			"relay.test": {net.ParseIP("93.184.216.35")},
		},
	}
	req := &http.Request{
		URL:    mustURL(t, "https://start.test/3"),
		Header: http.Header{testAuthorization: []string{testBearerToken}},
	}
	via := []*http.Request{
		{URL: mustURL(t, "https://start.test/1")},
		{URL: mustURL(t, "https://relay.test/2")},
	}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 5})(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}

	if got := req.Header.Get(testAuthorization); got != testBearerToken {
		t.Fatalf("return-to-origin Authorization = %q, want preserved", got)
	}
}

func TestRedirectPolicyStripsCredentialsOutsideSameOrigin(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			testExampleCom: {net.ParseIP("93.184.216.34")},
			"other.com":    {net.ParseIP("93.184.216.35")},
		},
	}
	tests := []struct {
		name       string
		from       string
		to         string
		wantHeader bool
	}{
		{name: "same origin", from: testHTTPSExampleComStart, to: "https://example.com/next", wantHeader: true},
		{name: "same origin default port", from: testHTTPSExampleComStart, to: "https://example.com:443/next", wantHeader: true},
		{name: "different port", from: testHTTPSExampleComStart, to: "https://example.com:8443/next"},
		{name: "scheme downgrade", from: testHTTPSExampleComStart, to: "http://example.com/next"},
		{name: "cross host", from: testHTTPSExampleComStart, to: "https://other.com/next"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &http.Request{
				URL: mustURL(t, tt.to),
				Header: http.Header{
					testAuthorization: []string{testBearerToken},
					"Cookie":          []string{"session=abc"},
				},
			}
			via := []*http.Request{{URL: mustURL(t, tt.from)}}

			if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 3})(req, via); err != nil {
				t.Fatalf("RedirectPolicy() error = %v", err)
			}

			gotAuth := req.Header.Get(testAuthorization)
			gotCookie := req.Header.Get("Cookie")

			if tt.wantHeader {
				if gotAuth != testBearerToken || gotCookie != "session=abc" {
					t.Fatalf("same-origin redirect headers = (%q, %q), want preserved", gotAuth, gotCookie)
				}

				return
			}

			if gotAuth != "" || gotCookie != "" {
				t.Fatalf("cross-origin redirect headers = (%q, %q), want empty", gotAuth, gotCookie)
			}
		})
	}
}

func TestRedirectPolicyForwardHeadersKeepsCrossOriginHeaders(t *testing.T) {
	t.Parallel()

	policy := Policy{Resolver: staticResolver{"other.com": {net.ParseIP("93.184.216.35")}}}
	req := &http.Request{
		URL:    mustURL(t, "https://other.com:8443/next"),
		Header: http.Header{testAuthorization: []string{testBearerToken}},
	}
	via := []*http.Request{{URL: mustURL(t, testHTTPSExampleComStart)}}

	cfg := RedirectConfig{Policy: policy, MaxRedirects: 3, ForwardHeaders: true}
	if err := RedirectPolicy(cfg)(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}

	if got := req.Header.Get(testAuthorization); got != testBearerToken {
		t.Fatalf("ForwardHeaders redirect Authorization = %q, want preserved", got)
	}
}

func TestRedirectPolicyValidatesTargetAndStripsCrossHostHeaders(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			testExampleCom: {net.ParseIP("93.184.216.34")},
			"other.com":    {net.ParseIP("93.184.216.35")},
		},
	}
	req := &http.Request{
		URL:    mustURL(t, "https://other.com/final"),
		Header: http.Header{testAuthorization: []string{testBearerToken}},
	}
	via := []*http.Request{{URL: mustURL(t, testHTTPSExampleComStart)}}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 2})(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}

	if got := req.Header.Get(testAuthorization); got != "" {
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
	policy := Policy{Resolver: staticResolver{testExampleCom: {net.ParseIP("93.184.216.34")}}}

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

type sequenceResolver struct {
	mu      sync.Mutex
	answers [][]net.IP
	calls   int
}

func (r *sequenceResolver) LookupIP(_ context.Context, _, _ string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	index := min(r.calls, len(r.answers)-1)
	r.calls++

	return r.answers[index], nil
}

func (r *sequenceResolver) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls
}

type trackedBody struct {
	mu     sync.Mutex
	closed int
}

func (b *trackedBody) Read([]byte) (int, error) { return 0, io.EOF }

func (b *trackedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed++

	return nil
}

func (b *trackedBody) Closed() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.closed
}

type dialGuardedRoundTripper struct {
	calls int
}

func (*dialGuardedRoundTripper) NetguardDialGuarded() bool { return true }

func (d *dialGuardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	d.calls++

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := f(req)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return out, nil
}

func callGuardedDial(dial func(context.Context, string, string) (net.Conn, error), address string) error {
	if _, err := dial(context.Background(), "tcp", address); err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	return nil
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}

	return parsed
}

func TestRedirectPolicyStripsNonCanonicalHeaderKeys(t *testing.T) {
	t.Parallel()

	policy := Policy{
		Resolver: staticResolver{
			testExampleCom: {net.ParseIP("93.184.216.34")},
			"other.com":    {net.ParseIP("93.184.216.35")},
		},
	}
	req := &http.Request{
		URL: mustURL(t, "https://other.com/next"),
		Header: http.Header{
			"x-internal-token": []string{"secret"},
			"authorization":    []string{testBearerToken},
			testAuthorization:  []string{"Bearer canonical"},
		},
	}
	via := []*http.Request{{URL: mustURL(t, testHTTPSExampleComStart)}}

	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 3})(req, via); err != nil {
		t.Fatalf("RedirectPolicy() error = %v", err)
	}

	if len(req.Header) != 0 {
		t.Fatalf("cross-origin redirect header = %v, want empty", req.Header)
	}

	nilHeaderReq := &http.Request{URL: mustURL(t, "https://other.com/next")}
	if err := RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 3})(nilHeaderReq, via); err != nil {
		t.Fatalf("RedirectPolicy() nil header error = %v", err)
	}
}

func TestRedirectPolicyStripsNonCanonicalHeaderKeysOnWire(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		gotToken string
	)

	final := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()

		gotToken = r.Header.Get("X-Internal-Token")
		mu.Unlock()
	}))

	defer final.Close()

	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/next", http.StatusFound)
	}))
	defer start.Close()

	policy := Policy{
		AllowedIPPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Schemes:           []string{"http"},
		Timeout:           5 * time.Second,
	}
	client := GuardedClient(nil, policy)

	client.CheckRedirect = RedirectPolicy(RedirectConfig{Policy: policy, MaxRedirects: 3})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, start.URL+"/start", http.NoBody)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}

	req.Header["x-internal-token"] = []string{"secret"}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("Body.Close() error = %v", closeErr)
		}
	}()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Errorf("Copy() error = %v", err)
	}

	testsupport.CloseNow(t, "resp.Body.Close", resp.Body.Close)

	mu.Lock()
	defer mu.Unlock()

	if gotToken != "" {
		t.Fatalf("cross-origin hop received X-Internal-Token = %q, want empty", gotToken)
	}
}

func TestIsBlockedAddrZonedAddressFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []string{
		"fc00::1%eth0",
		"64:ff9b::a9fe:a9fe%eth0",
		"fe80::1%eth0",
		"::1%lo",
		"ff02::1%eth0",
		"2001:db8::1%eth0",
		"::ffff:169.254.169.254%eth0",
		"2001:4860:4860::8888%eth0",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			addr, err := netip.ParseAddr(raw)
			if err != nil {
				t.Fatalf("netip.ParseAddr(%q) error = %v", raw, err)
			}

			if !IsBlockedAddr(addr) {
				t.Fatalf("IsBlockedAddr(%q) = false, want true", raw)
			}
		})
	}
}
