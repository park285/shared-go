package httputil

import (
	"net/http"
	"testing"
	"time"
)

func TestApplyTransportProfile_MaxIdleConnsApplied(t *testing.T) {
	t.Parallel()

	transport := &http.Transport{MaxIdleConns: 100}
	applyTransportProfile(transport, TransportProfile{MaxIdleConns: 200})

	if transport.MaxIdleConns != 200 {
		t.Fatalf("MaxIdleConns = %d, want 200", transport.MaxIdleConns)
	}
}

func TestApplyTransportProfile_MaxIdleConnsZeroPreservesDefault(t *testing.T) {
	t.Parallel()

	transport := &http.Transport{MaxIdleConns: 100}
	applyTransportProfile(transport, TransportProfile{})

	if transport.MaxIdleConns != 100 {
		t.Fatalf("MaxIdleConns = %d, want 100 (zero value must preserve default)", transport.MaxIdleConns)
	}
}

func TestExternalAPITransportProfile_PoolPin(t *testing.T) {
	t.Parallel()

	transport := mustClientTransport(t, NewExternalAPIClient(time.Second))

	if transport.MaxIdleConns != 128 {
		t.Fatalf("external MaxIdleConns = %d, want 128", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost != 32 {
		t.Fatalf("external MaxConnsPerHost = %d, want 32", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != 16 {
		t.Fatalf("external MaxIdleConnsPerHost = %d, want 16", transport.MaxIdleConnsPerHost)
	}
	if transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("external TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, 5*time.Second)
	}
	if transport.ResponseHeaderTimeout != 15*time.Second {
		t.Fatalf("external ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, 15*time.Second)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("external IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, 90*time.Second)
	}
}

func TestInternalServiceTransportProfile_PoolPin(t *testing.T) {
	t.Parallel()

	transport := mustClientTransport(t, NewInternalServiceClient(time.Second))

	if transport.MaxIdleConns != 256 {
		t.Fatalf("internal MaxIdleConns = %d, want 256", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost != 64 {
		t.Fatalf("internal MaxConnsPerHost = %d, want 64", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != 32 {
		t.Fatalf("internal MaxIdleConnsPerHost = %d, want 32", transport.MaxIdleConnsPerHost)
	}
	if transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("internal TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, 5*time.Second)
	}
	if transport.ResponseHeaderTimeout != 10*time.Second {
		t.Fatalf("internal ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, 10*time.Second)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("internal IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, 90*time.Second)
	}
}
