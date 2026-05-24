package httputil

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	const timeout = 15 * time.Second
	client := NewClient(timeout)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.Timeout != timeout {
		t.Fatalf("NewClient() timeout = %s, want %s", client.Timeout, timeout)
	}
}

func TestDefaultClient(t *testing.T) {
	t.Parallel()

	client := DefaultClient()
	if client == nil {
		t.Fatal("DefaultClient() returned nil")
	}

	const wantTimeout = 30 * time.Second
	if client.Timeout != wantTimeout {
		t.Fatalf("DefaultClient() timeout = %s, want %s", client.Timeout, wantTimeout)
	}
}

func TestNewProfiledClient(t *testing.T) {
	t.Parallel()

	client := NewProfiledClient(TransportProfile{
		Timeout:               20 * time.Second,
		DialTimeout:           3 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       6 * time.Second,
		MaxConnsPerHost:       7,
		MaxIdleConnsPerHost:   8,
		DisableHTTP2:          true,
	})
	if client == nil {
		t.Fatal("NewProfiledClient() returned nil")
	}
	if client.Timeout != 20*time.Second {
		t.Fatalf("client.Timeout = %s, want %s", client.Timeout, 20*time.Second)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSHandshakeTimeout != 4*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, 4*time.Second)
	}
	if transport.ResponseHeaderTimeout != 5*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, 5*time.Second)
	}
	if transport.IdleConnTimeout != 6*time.Second {
		t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, 6*time.Second)
	}
	if transport.MaxConnsPerHost != 7 {
		t.Fatalf("MaxConnsPerHost = %d, want 7", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != 8 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 8", transport.MaxIdleConnsPerHost)
	}
	if transport.TLSNextProto == nil {
		t.Fatal("TLSNextProto should be non-nil when HTTP/2 disabled")
	}
}

func TestNewExternalAPIClient(t *testing.T) {
	t.Parallel()

	client := NewExternalAPIClient(12 * time.Second)
	if client == nil {
		t.Fatal("NewExternalAPIClient() returned nil")
	}
	if client.Timeout != 12*time.Second {
		t.Fatalf("Timeout = %s, want %s", client.Timeout, 12*time.Second)
	}
	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
}

func TestNewInternalServiceClient(t *testing.T) {
	t.Parallel()

	client := NewInternalServiceClient(8 * time.Second)
	if client == nil {
		t.Fatal("NewInternalServiceClient() returned nil")
	}
	if client.Timeout != 8*time.Second {
		t.Fatalf("Timeout = %s, want %s", client.Timeout, 8*time.Second)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.MaxConnsPerHost != 64 {
		t.Fatalf("MaxConnsPerHost = %d, want 64", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != 32 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 32", transport.MaxIdleConnsPerHost)
	}
}
