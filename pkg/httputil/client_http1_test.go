package httputil

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestBaseProfiledTransportKeepsTLSALPNHTTP1Only(t *testing.T) {
	t.Parallel()

	requireHTTP1OnlyTLSALPN(t, baseProfiledTransport())
}

func TestNewExternalAPIClientKeepsTLSALPNHTTP1Only(t *testing.T) {
	t.Parallel()

	requireHTTP1OnlyTLSALPN(t, mustClientTransport(t, NewExternalAPIClient(time.Second)))
}

func TestNewInternalServiceClientKeepsTLSALPNHTTP1Only(t *testing.T) {
	t.Parallel()

	requireHTTP1OnlyTLSALPN(t, mustClientTransport(t, NewInternalServiceClient(time.Second)))
}

func TestNewExternalAPIClientTLSClientHelloOffersOnlyHTTP1(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		offered []string
	)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			mu.Lock()

			offered = slices.Clone(hello.SupportedProtos)

			mu.Unlock()

			return nil, nil //nolint:nilnil // GetConfigForClient는 nil config와 nil error로 부모 TLS 설정을 쓰라는 계약이다.
		},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	client := NewExternalAPIClient(2 * time.Second)
	transport := mustClientTransport(t, client)
	trustTestServer(t, transport, server)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatalf("create GET request: %v", err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET test server: %v", err)
	}

	if response == nil {
		t.Fatal("GET test server returned a nil response")
	}

	defer response.Body.Close()

	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}

	mu.Lock()

	got := slices.Clone(offered)

	mu.Unlock()

	requireHTTP1OnlyALPNs(t, got)
}

func trustTestServer(t *testing.T, transport *http.Transport, server *httptest.Server) {
	t.Helper()

	serverTransport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("server client transport type = %T, want *http.Transport", server.Client().Transport)
	}

	roots := serverTransport.TLSClientConfig.RootCAs
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}

		return
	}

	cfg := transport.TLSClientConfig.Clone()

	cfg.RootCAs = roots
	transport.TLSClientConfig = cfg
}

func requireHTTP1OnlyTLSALPN(t *testing.T, transport *http.Transport) {
	t.Helper()

	if transport == nil {
		t.Fatal("transport is nil")
	}

	if transport.Protocols == nil || !transport.Protocols.HTTP1() || transport.Protocols.String() != "{HTTP1}" {
		t.Fatalf("Protocols = %v, want {HTTP1}", transport.Protocols)
	}

	if transport.TLSClientConfig == nil {
		return
	}

	requireHTTP1OnlyALPNs(t, transport.TLSClientConfig.NextProtos)
}

func requireHTTP1OnlyALPNs(t *testing.T, protos []string) {
	t.Helper()

	for _, proto := range protos {
		if proto != "http/1.1" {
			t.Fatalf("ALPN contains %q, want empty or only http/1.1", proto)
		}
	}
}
