package healthprobe

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/park285/shared-go/transport/internal/testsupport"

	"github.com/park285/shared-go/v2/pkg/netguard"
)

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

func TestHealthprobeMapsNetguardRedirectLimit(t *testing.T) {
	t.Parallel()

	req := &http.Request{URL: mustParseURL(t, "https://example.com/final")}
	via := make([]*http.Request, maxRedirects)

	for index := range via {
		via[index] = &http.Request{URL: mustParseURL(t, "https://example.com/hop")}
	}

	err := redirectPolicy(fetchOptions{FollowRedirects: true}, nil)(req, via)
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

	opts := fetchOptions{MaxBodyBytes: 256, FollowRedirects: true}
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
