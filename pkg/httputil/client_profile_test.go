package httputil

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

func TestApplyTransportProfile(t *testing.T) {
	t.Parallel()

	t.Run("모든 양수 필드를 transport에 반영", func(t *testing.T) {
		t.Parallel()

		sentinelErr := errors.New("sentinel dial")
		transport := &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, sentinelErr
			},
		}
		profile := TransportProfile{
			DialTimeout:           2 * time.Second,
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: 4 * time.Second,
			IdleConnTimeout:       5 * time.Second,
			MaxConnsPerHost:       6,
			MaxIdleConnsPerHost:   7,
		}

		applyTransportProfile(transport, profile)

		if transport.DialContext == nil {
			t.Fatal("DialContext is nil")
		}
		requireDialContextTimeout(t, transport.DialContext, profile.DialTimeout)
		_, err := transport.DialContext(context.Background(), "", "")
		if errors.Is(err, sentinelErr) {
			t.Fatal("DialContext was not replaced")
		}
		if transport.TLSHandshakeTimeout != profile.TLSHandshakeTimeout {
			t.Fatalf("TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, profile.TLSHandshakeTimeout)
		}
		if transport.ResponseHeaderTimeout != profile.ResponseHeaderTimeout {
			t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, profile.ResponseHeaderTimeout)
		}
		if transport.IdleConnTimeout != profile.IdleConnTimeout {
			t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, profile.IdleConnTimeout)
		}
		if transport.MaxConnsPerHost != profile.MaxConnsPerHost {
			t.Fatalf("MaxConnsPerHost = %d, want %d", transport.MaxConnsPerHost, profile.MaxConnsPerHost)
		}
		if transport.MaxIdleConnsPerHost != profile.MaxIdleConnsPerHost {
			t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, profile.MaxIdleConnsPerHost)
		}
	})

	t.Run("zero 필드는 기존 값을 유지", func(t *testing.T) {
		t.Parallel()

		sentinelErr := errors.New("sentinel dial")
		transport := &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, sentinelErr
			},
			TLSHandshakeTimeout:   11 * time.Second,
			ResponseHeaderTimeout: 12 * time.Second,
			IdleConnTimeout:       13 * time.Second,
			MaxConnsPerHost:       14,
			MaxIdleConnsPerHost:   15,
		}

		applyTransportProfile(transport, TransportProfile{})

		_, err := transport.DialContext(context.Background(), "tcp", "example.com:80")
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("DialContext error = %v, want sentinel", err)
		}
		if transport.TLSHandshakeTimeout != 11*time.Second {
			t.Fatalf("TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, 11*time.Second)
		}
		if transport.ResponseHeaderTimeout != 12*time.Second {
			t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, 12*time.Second)
		}
		if transport.IdleConnTimeout != 13*time.Second {
			t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, 13*time.Second)
		}
		if transport.MaxConnsPerHost != 14 {
			t.Fatalf("MaxConnsPerHost = %d, want 14", transport.MaxConnsPerHost)
		}
		if transport.MaxIdleConnsPerHost != 15 {
			t.Fatalf("MaxIdleConnsPerHost = %d, want 15", transport.MaxIdleConnsPerHost)
		}
	})

	t.Run("positive DialTimeout preserves zero fields", func(t *testing.T) {
		t.Parallel()

		sentinelErr := errors.New("sentinel dial")
		transport := &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, sentinelErr
			},
			TLSHandshakeTimeout:   11 * time.Second,
			ResponseHeaderTimeout: 12 * time.Second,
			IdleConnTimeout:       13 * time.Second,
			MaxConnsPerHost:       14,
			MaxIdleConnsPerHost:   15,
		}
		profile := TransportProfile{DialTimeout: 3 * time.Second}

		applyTransportProfile(transport, profile)

		requireDialContextTimeout(t, transport.DialContext, profile.DialTimeout)
		if transport.TLSHandshakeTimeout != 11*time.Second {
			t.Fatalf("TLSHandshakeTimeout = %s, want %s", transport.TLSHandshakeTimeout, 11*time.Second)
		}
		if transport.ResponseHeaderTimeout != 12*time.Second {
			t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, 12*time.Second)
		}
		if transport.IdleConnTimeout != 13*time.Second {
			t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, 13*time.Second)
		}
		if transport.MaxConnsPerHost != 14 {
			t.Fatalf("MaxConnsPerHost = %d, want 14", transport.MaxConnsPerHost)
		}
		if transport.MaxIdleConnsPerHost != 15 {
			t.Fatalf("MaxIdleConnsPerHost = %d, want 15", transport.MaxIdleConnsPerHost)
		}
	})
}

func TestApplyTransportProfileHTTP2(t *testing.T) {
	t.Parallel()

	t.Run("DisableHTTP2 true sets empty TLSNextProto", func(t *testing.T) {
		t.Parallel()

		transport := &http.Transport{
			TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{
				"h2": func(string, *tls.Conn) http.RoundTripper { return nil },
			},
		}

		applyTransportProfile(transport, TransportProfile{DisableHTTP2: true})

		if transport.TLSNextProto == nil {
			t.Fatal("TLSNextProto is nil")
		}
		if len(transport.TLSNextProto) != 0 {
			t.Fatalf("TLSNextProto len = %d, want 0", len(transport.TLSNextProto))
		}
	})

	t.Run("DisableHTTP2 false preserves TLSNextProto", func(t *testing.T) {
		t.Parallel()

		customHandler := func(string, *tls.Conn) http.RoundTripper { return nil }
		baseline := map[string]func(string, *tls.Conn) http.RoundTripper{
			"custom": customHandler,
		}
		transport := &http.Transport{
			TLSNextProto: baseline,
		}
		baselineMapPointer := tlsNextProtoMapPointer(baseline)
		baselineHandlerPointer := reflect.ValueOf(customHandler).Pointer()

		applyTransportProfile(transport, TransportProfile{DisableHTTP2: false})

		if tlsNextProtoMapPointer(transport.TLSNextProto) != baselineMapPointer {
			t.Fatal("TLSNextProto map identity was not preserved")
		}
		if len(transport.TLSNextProto) != 1 {
			t.Fatalf("TLSNextProto len = %d, want 1", len(transport.TLSNextProto))
		}
		custom, ok := transport.TLSNextProto["custom"]
		if !ok {
			t.Fatal("TLSNextProto custom entry was not preserved")
		}
		if reflect.ValueOf(custom).Pointer() != baselineHandlerPointer {
			t.Fatal("TLSNextProto custom handler identity was not preserved")
		}
	})
}

func TestBaseProfiledTransportUsesDefaultBaseline(t *testing.T) {
	t.Parallel()

	got := baseProfiledTransport()
	if got == nil {
		t.Fatal("baseProfiledTransport() returned nil")
	}

	want, ok := http.DefaultTransport.(*http.Transport)
	if !ok || want == nil {
		t.Fatal("http.DefaultTransport is not *http.Transport")
	}

	if got.Proxy == nil {
		t.Fatal("Proxy is nil")
	}
	if reflect.ValueOf(got.Proxy).Pointer() != reflect.ValueOf(want.Proxy).Pointer() {
		t.Fatal("Proxy does not match http.DefaultTransport")
	}
	if got.ForceAttemptHTTP2 != want.ForceAttemptHTTP2 {
		t.Fatalf("ForceAttemptHTTP2 = %v, want %v", got.ForceAttemptHTTP2, want.ForceAttemptHTTP2)
	}
	if got.MaxIdleConns != want.MaxIdleConns {
		t.Fatalf("MaxIdleConns = %d, want %d", got.MaxIdleConns, want.MaxIdleConns)
	}
	if got.ExpectContinueTimeout != want.ExpectContinueTimeout {
		t.Fatalf("ExpectContinueTimeout = %s, want %s", got.ExpectContinueTimeout, want.ExpectContinueTimeout)
	}
	if clone := got.Clone(); clone == got || clone == nil {
		t.Fatal("Clone() did not return a distinct transport")
	}
}

func TestNewProfiledClientTimeout(t *testing.T) {
	t.Parallel()

	profile := TransportProfile{Timeout: 17 * time.Second}
	client := NewProfiledClient(profile)

	if client.Timeout != profile.Timeout {
		t.Fatalf("Timeout = %s, want %s", client.Timeout, profile.Timeout)
	}
}

func TestProfiledClientFactoryDifferences(t *testing.T) {
	t.Parallel()

	externalTransport := mustClientTransport(t, NewExternalAPIClient(time.Second))
	internalTransport := mustClientTransport(t, NewInternalServiceClient(time.Second))

	if externalTransport.MaxConnsPerHost != 32 {
		t.Fatalf("external MaxConnsPerHost = %d, want 32", externalTransport.MaxConnsPerHost)
	}
	if externalTransport.MaxIdleConnsPerHost != 16 {
		t.Fatalf("external MaxIdleConnsPerHost = %d, want 16", externalTransport.MaxIdleConnsPerHost)
	}
	if internalTransport.MaxConnsPerHost != 64 {
		t.Fatalf("internal MaxConnsPerHost = %d, want 64", internalTransport.MaxConnsPerHost)
	}
	if internalTransport.MaxIdleConnsPerHost != 32 {
		t.Fatalf("internal MaxIdleConnsPerHost = %d, want 32", internalTransport.MaxIdleConnsPerHost)
	}
	if externalTransport.ResponseHeaderTimeout != 15*time.Second {
		t.Fatalf("external ResponseHeaderTimeout = %s, want %s", externalTransport.ResponseHeaderTimeout, 15*time.Second)
	}
	if internalTransport.ResponseHeaderTimeout != 10*time.Second {
		t.Fatalf("internal ResponseHeaderTimeout = %s, want %s", internalTransport.ResponseHeaderTimeout, 10*time.Second)
	}
}

func mustClientTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()

	if client == nil {
		t.Fatal("client is nil")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	return transport
}

func requireDialContextTimeout(t *testing.T, dialContext func(context.Context, string, string) (net.Conn, error), want time.Duration) {
	t.Helper()

	if dialContext == nil {
		t.Fatal("DialContext is nil")
	}
	funcValuePointer := *(*unsafe.Pointer)(unsafe.Pointer(&dialContext))
	if funcValuePointer == nil {
		t.Fatal("DialContext func value is nil")
	}
	fields := (*[2]unsafe.Pointer)(funcValuePointer)
	dialer := (*net.Dialer)(fields[1])
	if dialer == nil {
		t.Fatal("DialContext dialer capture is nil")
	}
	if dialer.Timeout != want {
		t.Fatalf("DialContext dialer timeout = %s, want %s", dialer.Timeout, want)
	}
}

func tlsNextProtoMapPointer(m map[string]func(string, *tls.Conn) http.RoundTripper) uintptr {
	return *(*uintptr)(unsafe.Pointer(&m))
}
