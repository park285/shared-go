package healthprobe

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewClientHTTPCloseFnClosesIdleConnections(t *testing.T) {
	closed := make(chan struct{}, 8)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()

	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	client, closeFn, err := newClient(parsed, FetchOptions{})
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("newClient() transport = %T, want dedicated *http.Transport", client.Transport)
	}

	// loopback은 dialGuard가 막으므로 테스트에서만 dial 경로를 우회한다.
	transport.DialContext = (&net.Dialer{}).DialContext

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain body error = %v", err)
	}

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body error = %v", err)
	}

	closeFn()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("closeFn() did not close the idle connection")
	}
}

func TestNewClientHTTPKeepsSharedTransportWhenPrivateAllowed(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("http://internal.test/health")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	client, closeFn, err := newClient(parsed, internalFetchOptions())
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	if client.Transport != nil {
		t.Fatalf("newClient() transport = %T, want shared default transport", client.Transport)
	}

	closeFn()
}
