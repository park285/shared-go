package healthprobe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchURL_DNSRebinding_6ccdf328(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("rebound"))
	}))
	defer server.Close()

	orig := lookupIPAddr
	lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	defer func() { lookupIPAddr = orig }()

	opts := FetchOptions{RestrictPrivateNetworks: true}
	if _, err := FetchURLWithOptions(server.URL, nil, opts); !errors.Is(err, ErrPrivateNetwork) {
		t.Fatalf("rebinding fetch error = %v, want ErrPrivateNetwork (dial-time guard must reject loopback)", err)
	}
}

func TestSG04HealthprobeRejectsLoopbackByDefault_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := FetchOptions{RestrictPrivateNetworks: true}
	if _, err := FetchURLWithOptions(server.URL, nil, opts); !errors.Is(err, ErrPrivateNetwork) {
		t.Fatalf("FetchURLWithOptions(loopback, RestrictPrivateNetworks) error = %v, want ErrPrivateNetwork", err)
	}
}

func TestSG04HealthprobeRejectsLinkLocalMetadata_02aae1e0(t *testing.T) {
	t.Parallel()

	opts := FetchOptions{RestrictPrivateNetworks: true}
	if _, err := FetchURLWithOptions("http://169.254.169.254/latest/meta-data/", nil, opts); !errors.Is(err, ErrPrivateNetwork) {
		t.Fatalf("FetchURLWithOptions(link-local) error = %v, want ErrPrivateNetwork", err)
	}
}

func TestSG04HealthprobeRejectsHostNotInAllowlist_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := FetchOptions{AllowedHosts: []string{"only.example.com"}}
	if _, err := FetchURLWithOptions(server.URL, nil, opts); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("FetchURLWithOptions(non-allowlisted) error = %v, want ErrHostNotAllowed", err)
	}
}

func TestSG04HealthprobeAllowsConfiguredHost_02aae1e0(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	opts := FetchOptions{AllowedHosts: []string{"127.0.0.1"}}
	body, err := FetchURLWithOptions(server.URL, nil, opts)
	if err != nil {
		t.Fatalf("FetchURLWithOptions(allowlisted 127.0.0.1) error = %v, want nil", err)
	}
	if string(body) != "ok" {
		t.Fatalf("FetchURLWithOptions body = %q, want ok", body)
	}
}

func TestSG04HealthprobeDoesNotForwardHeadersOnCrossHostRedirect_cfe49bff(t *testing.T) {
	t.Parallel()

	var gotAuth, gotAPIKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte("done"))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossHost := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
		http.Redirect(w, r, crossHost, http.StatusFound)
	}))
	defer redirector.Close()

	headers := map[string]string{
		"Authorization": "Bearer super-secret-token",
		"X-API-Key":     "probe-secret",
	}
	if _, err := FetchURLWithHeaders(redirector.URL, headers); err != nil {
		t.Fatalf("FetchURLWithHeaders(redirect) error = %v, want nil", err)
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
		_, _ = w.Write([]byte("ok"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	headers := map[string]string{"X-API-Key": "probe-secret"}
	if _, err := FetchURLWithHeaders(server.URL+"/start", headers); err != nil {
		t.Fatalf("FetchURLWithHeaders(same-host redirect) error = %v, want nil", err)
	}
	if gotAPIKey != "probe-secret" {
		t.Fatalf("same-host redirect X-API-Key = %q, want probe-secret (must be preserved)", gotAPIKey)
	}
}

func TestSG04HealthprobeBodyLimit_12bf48a7(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer server.Close()

	opts := FetchOptions{MaxBodyBytes: 256, FollowRedirects: true}
	if _, err := FetchURLWithOptions(server.URL, nil, opts); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("FetchURLWithOptions(1KiB body, 256 cap) error = %v, want ErrBodyTooLarge", err)
	}

	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 200))
	}))
	defer small.Close()
	if _, err := FetchURLWithOptions(small.URL, nil, opts); err != nil {
		t.Fatalf("FetchURLWithOptions(200B body, 256 cap) error = %v, want nil", err)
	}
}
