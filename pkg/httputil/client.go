package httputil

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

type TransportProfile struct {
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxConnsPerHost       int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
}

var externalAPITransportProfile = TransportProfile{
	DialTimeout:           5 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 15 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxConnsPerHost:       32,
	MaxIdleConnsPerHost:   16,
}

var internalServiceTransportProfile = TransportProfile{
	DialTimeout:           3 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxConnsPerHost:       64,
	MaxIdleConnsPerHost:   32,
}

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// 기본 keep-alive, proxy, TLS 기본 동작은 유지하고 timeout/pool/HTTP2 정책만 profile로 주입합니다.
func NewProfiledClient(profile TransportProfile) *http.Client {
	transport := baseProfiledTransport().Clone()
	applyTransportProfile(transport, profile)

	return &http.Client{
		Timeout:   profile.Timeout,
		Transport: transport,
	}
}

func baseProfiledTransport() *http.Transport {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if ok && baseTransport != nil {
		return baseTransport
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func applyTransportProfile(transport *http.Transport, profile TransportProfile) {
	if profile.DialTimeout > 0 {
		transport.DialContext = (&net.Dialer{
			Timeout: profile.DialTimeout,
		}).DialContext
	}
	if profile.TLSHandshakeTimeout > 0 {
		transport.TLSHandshakeTimeout = profile.TLSHandshakeTimeout
	}
	if profile.ResponseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = profile.ResponseHeaderTimeout
	}
	if profile.IdleConnTimeout > 0 {
		transport.IdleConnTimeout = profile.IdleConnTimeout
	}
	if profile.MaxConnsPerHost > 0 {
		transport.MaxConnsPerHost = profile.MaxConnsPerHost
	}
	if profile.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = profile.MaxIdleConnsPerHost
	}
	if profile.DisableHTTP2 {
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}
}

func NewExternalAPIClient(timeout time.Duration) *http.Client {
	profile := externalAPITransportProfile
	profile.Timeout = timeout
	return NewProfiledClient(profile)
}

func NewInternalServiceClient(timeout time.Duration) *http.Client {
	profile := internalServiceTransportProfile
	profile.Timeout = timeout
	return NewProfiledClient(profile)
}

func DefaultClient() *http.Client {
	return NewExternalAPIClient(30 * time.Second)
}
