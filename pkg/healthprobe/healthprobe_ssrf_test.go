package healthprobe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
	"github.com/park285/shared-go/v2/pkg/netguard"
)

func TestFetchURL_DNSRebinding_6ccdf328(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "rebound")
	}))
	defer server.Close()

	orig := lookupIPAddr

	lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}

	defer func() { lookupIPAddr = orig }()

	opts := fetchOptions{}
	if _, err := fetchURL(t.Context(), server.URL, nil, opts); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("rebinding fetch error = %v, want errPrivateNetwork (dial-time guard must reject loopback)", err)
	}
}

func TestSG04HealthprobeRejectsLoopbackByDefault_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := fetchOptions{}
	if _, err := fetchURL(t.Context(), server.URL, nil, opts); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("fetchURL(loopback, zero-value fetchOptions{}) error = %v, want errPrivateNetwork", err)
	}
}

func TestSG04FetchURLWithOptionsSecureByDefault_dd8840c(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "ok")
	}))
	defer server.Close()

	if _, err := fetchURL(t.Context(), server.URL, nil, fetchOptions{}); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("fetchURL(loopback, fetchOptions{}) error = %v, want errPrivateNetwork (zero value must block private networks)", err)
	}

	body, err := fetchURL(t.Context(), server.URL, nil, fetchOptions{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatalf("fetchURL(loopback, AllowPrivateNetworks=true) error = %v, want nil", err)
	}

	if string(body) != "ok" {
		t.Fatalf("fetchURL body = %q, want ok", body)
	}
}

func TestSG04DefaultAPIBlocksLoopback_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := fetchURL(t.Context(), server.URL, nil, defaultFetchOptions()); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("fetchURL(loopback, default options) error = %v, want errPrivateNetwork (secure-by-default)", err)
	}

	if err := CheckURL(t.Context(), server.URL); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("CheckURL(loopback) error = %v, want errPrivateNetwork (secure-by-default)", err)
	}

	if _, err := fetchURL(t.Context(), server.URL, map[string]string{"X-K": "v"}, defaultFetchOptions()); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("fetchURL(loopback, with headers) error = %v, want errPrivateNetwork (secure-by-default)", err)
	}
}

func TestSG04InternalAPIAllowsLoopback_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "ok")
	}))
	defer server.Close()

	body, err := fetchURL(t.Context(), server.URL, nil, internalFetchOptions(tlsOverrides{}))
	if err != nil {
		t.Fatalf("fetchURL(loopback, internal options) error = %v, want nil", err)
	}

	if string(body) != "ok" {
		t.Fatalf("fetchURL body = %q, want ok", body)
	}

	if _, err := fetchURL(t.Context(), server.URL, nil, internalFetchOptions(tlsOverrides{})); err != nil {
		t.Fatalf("fetchURL(loopback, internal options) error = %v, want nil", err)
	}

	if _, err := fetchURL(t.Context(), server.URL, map[string]string{"X-K": "v"}, internalFetchOptions(tlsOverrides{})); err != nil {
		t.Fatalf("fetchURL(loopback, headers, internal options) error = %v, want nil", err)
	}
}

func TestSG04ExternalAddressNotClassifiedPrivate_02aae1e0(t *testing.T) {
	t.Parallel()

	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
		if err := dialGuard(net.ParseIP(ip)); err != nil {
			t.Fatalf("dialGuard(%s) error = %v, want nil (external target must pass default guard)", ip, err)
		}
	}

	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "203.0.113.10", "2001:db8::1", "::1"} {
		if err := dialGuard(net.ParseIP(ip)); !errors.Is(err, errPrivateNetwork) {
			t.Fatalf("dialGuard(%s) error = %v, want errPrivateNetwork (must be blocked by default)", ip, err)
		}
	}
}

func TestSG04HealthprobeRejectsLinkLocalMetadata_02aae1e0(t *testing.T) {
	t.Parallel()

	opts := fetchOptions{}
	if _, err := fetchURL(t.Context(), "http://169.254.169.254/latest/meta-data/", nil, opts); !errors.Is(err, errPrivateNetwork) {
		t.Fatalf("fetchURL(link-local) error = %v, want errPrivateNetwork", err)
	}
}

func TestHealthprobeUsesNetguardBlockedRanges(t *testing.T) {
	t.Parallel()

	for _, ip := range []string{
		"100.64.0.1",
		"198.18.0.1",
		"240.0.0.1",
		"192.0.2.1",
		"224.0.0.1",
	} {
		err := dialGuard(net.ParseIP(ip))
		if !errors.Is(err, errPrivateNetwork) {
			t.Fatalf("dialGuard(%s) error = %v, want errPrivateNetwork", ip, err)
		}

		if !errors.Is(err, netguard.ErrBlockedIP) {
			t.Fatalf("dialGuard(%s) error = %v, want netguard.ErrBlockedIP", ip, err)
		}
	}
}

func TestHealthprobeMapsNetguardPolicyErrors(t *testing.T) {
	t.Parallel()

	opts := fetchOptions{AllowedHosts: []string{"allowed.example"}}
	err := authorizeTarget(t.Context(), mustParseURL(t, "https://other.example/ready"), opts)

	if !errors.Is(err, errHostNotAllowed) || !errors.Is(err, netguard.ErrHostNotAllowed) {
		t.Fatalf("authorizeTarget() error = %v, want healthprobe and netguard host errors", err)
	}
}

func TestHealthprobeMapsNetguardRedirectLimit(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: mustParseURL(t, "https://example.com/final")}
	via := make([]*http.Request, maxRedirects)

	for index := range via {
		via[index] = &http.Request{URL: mustParseURL(t, "https://example.com/hop")}
	}

	err := redirectPolicy(fetchOptions{FollowRedirects: true, AllowPrivateNetworks: true}, nil)(req, via)
	if !errors.Is(err, errTooManyRedirects) || !errors.Is(err, netguard.ErrTooManyRedirects) {
		t.Fatalf("redirectPolicy() error = %v, want healthprobe and netguard redirect errors", err)
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}

	return parsed
}

func TestSG04HealthprobeRejectsHostNotInAllowlist_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := fetchOptions{AllowedHosts: []string{"only.example.com"}}
	if _, err := fetchURL(t.Context(), server.URL, nil, opts); !errors.Is(err, errHostNotAllowed) {
		t.Fatalf("fetchURL(non-allowlisted) error = %v, want errHostNotAllowed", err)
	}
}

func TestSG04HealthprobeAllowsConfiguredHost_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "ok")
	}))
	defer server.Close()

	opts := fetchOptions{AllowedHosts: []string{"127.0.0.1"}, AllowPrivateNetworks: true}

	body, err := fetchURL(t.Context(), server.URL, nil, opts)
	if err != nil {
		t.Fatalf("fetchURL(allowlisted 127.0.0.1) error = %v, want nil", err)
	}

	if string(body) != "ok" {
		t.Fatalf("fetchURL body = %q, want ok", body)
	}
}

func TestSG04HealthprobeDoesNotForwardHeadersOnCrossHostRedirect_cfe49bff(t *testing.T) {
	t.Parallel()

	var gotAuth, gotAPIKey string

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-API-Key")

		testsupport.WriteResponse(t, w, "done")
	}))

	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossHost := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
		http.Redirect(w, r, crossHost, http.StatusFound)
	}))
	defer redirector.Close()

	headers := map[string]string{
		"Authorization": "Bearer super-secret-token",
		"X-API-Key":     testProbeSecret,
	}
	if _, err := fetchURL(t.Context(), redirector.URL, headers, internalFetchOptions(tlsOverrides{})); err != nil {
		t.Fatalf("fetchURL(redirect) error = %v, want nil", err)
	}

	if gotAuth != "" {
		t.Fatalf("cross-host redirect forwarded Authorization header = %q, want empty", gotAuth)
	}

	if gotAPIKey != "" {
		t.Fatalf("cross-host redirect forwarded X-API-Key header = %q, want empty", gotAPIKey)
	}
}

func TestSG04HealthprobeKeepsHeadersOnSameHostRedirect_cfe49bff(t *testing.T) {
	t.Parallel()

	var gotAPIKey string

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")

		testsupport.WriteResponse(t, w, "ok")
	})

	server := httptest.NewServer(mux)

	defer server.Close()

	headers := map[string]string{"X-API-Key": testProbeSecret}
	if _, err := fetchURL(t.Context(), server.URL+"/start", headers, internalFetchOptions(tlsOverrides{})); err != nil {
		t.Fatalf("fetchURL(same-host redirect) error = %v, want nil", err)
	}

	if gotAPIKey != testProbeSecret {
		t.Fatalf("same-host redirect X-API-Key = %q, want probe-secret (must be preserved)", gotAPIKey)
	}
}

func TestSG04HealthprobeBodyLimit_12bf48a7(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteBytes(t, w, make([]byte, 1024))
	}))
	defer server.Close()

	opts := fetchOptions{MaxBodyBytes: 256, FollowRedirects: true, AllowPrivateNetworks: true}
	if _, err := fetchURL(t.Context(), server.URL, nil, opts); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("fetchURL(1KiB body, 256 cap) error = %v, want errBodyTooLarge", err)
	}

	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteBytes(t, w, make([]byte, 200))
	}))
	defer small.Close()

	if _, err := fetchURL(t.Context(), small.URL, nil, opts); err != nil {
		t.Fatalf("fetchURL(200B body, 256 cap) error = %v, want nil", err)
	}
}
