package httputil

import (
	"context"
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

		assertAllPositiveProfileFieldsApplied(t)
	})

	t.Run("zero 필드는 기존 값을 유지", func(t *testing.T) {
		t.Parallel()

		assertZeroProfileFieldsPreserveTransport(t)
	})

	t.Run("positive DialTimeout preserves zero fields", func(t *testing.T) {
		t.Parallel()

		assertDialTimeoutOnlyProfilePreservesZeroFields(t)
	})
}

func assertAllPositiveProfileFieldsApplied(t *testing.T) {
	t.Helper()

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

	_, err := transport.DialContext(t.Context(), "", "")
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
}

func assertZeroProfileFieldsPreserveTransport(t *testing.T) {
	t.Helper()

	sentinelErr := errors.New("sentinel dial")
	transport := newBaselineProfiledTransport(sentinelErr)

	applyTransportProfile(transport, TransportProfile{})

	_, err := transport.DialContext(t.Context(), "tcp", "example.com:80")
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("DialContext error = %v, want sentinel", err)
	}

	assertBaselineTransportFieldsPreserved(t, transport)
}

func assertDialTimeoutOnlyProfilePreservesZeroFields(t *testing.T) {
	t.Helper()

	transport := newBaselineProfiledTransport(errors.New("sentinel dial"))
	profile := TransportProfile{DialTimeout: 3 * time.Second}

	applyTransportProfile(transport, profile)

	requireDialContextTimeout(t, transport.DialContext, profile.DialTimeout)
	assertBaselineTransportFieldsPreserved(t, transport)
}

func newBaselineProfiledTransport(dialErr error) *http.Transport {
	return &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, dialErr
		},
		TLSHandshakeTimeout:   11 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		IdleConnTimeout:       13 * time.Second,
		MaxConnsPerHost:       14,
		MaxIdleConnsPerHost:   15,
	}
}

func assertBaselineTransportFieldsPreserved(t *testing.T, transport *http.Transport) {
	t.Helper()

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

	if got.Protocols == nil || !got.Protocols.HTTP1() {
		t.Fatal("baseProfiledTransport() must enable HTTP/1.1")
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

func TestBaseProfiledTransportIsolatedFromGlobal(t *testing.T) {
	t.Parallel()

	global, ok := http.DefaultTransport.(*http.Transport)
	if !ok || global == nil {
		t.Fatal("http.DefaultTransport is not *http.Transport")
	}

	got := baseProfiledTransport()
	if got == global {
		t.Fatal("baseProfiledTransport() returned the global http.DefaultTransport pointer")
	}

	beforeMaxIdle := global.MaxIdleConns

	got.MaxIdleConns = beforeMaxIdle + 4242
	got.MaxConnsPerHost = global.MaxConnsPerHost + 99

	if global.MaxIdleConns != beforeMaxIdle {
		t.Fatalf("mutating returned transport changed global.MaxIdleConns: %d, want %d", global.MaxIdleConns, beforeMaxIdle)
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

	funcValuePointer := *(*unsafe.Pointer)(unsafe.Pointer(&dialContext)) //nolint:gosec // 함수 값 동일성 검증을 위한 테스트 전용 포인터 비교다.
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
