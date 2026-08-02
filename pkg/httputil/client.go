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
	MaxIdleConns          int
	MaxConnsPerHost       int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
}

var externalAPITransportProfile = TransportProfile{
	DialTimeout:           5 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 15 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConns:          128,
	MaxConnsPerHost:       32,
	MaxIdleConnsPerHost:   16,
}

var internalServiceTransportProfile = TransportProfile{
	DialTimeout:           3 * time.Second,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConns:          256,
	MaxConnsPerHost:       64,
	MaxIdleConnsPerHost:   32,
}

// NewClient는 http.DefaultTransport를 그대로 공유하므로 connection pool(MaxIdleConnsPerHost=2)도
// 프로세스 내 다른 소비자와 공유됩니다. 같은 host로 동시 요청이 많은 소비자는
// NewInternalServiceClient 또는 NewExternalAPIClient를 사용해 전용 pool을 확보하십시오.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// 기본 keep-alive, proxy, TLS 기본 동작은 유지하고 timeout/pool/HTTP2 정책만 profile로 주입합니다.
func NewProfiledClient(profile TransportProfile) *http.Client {
	transport := baseProfiledTransport()
	applyTransportProfile(transport, profile)

	return &http.Client{
		Timeout:   profile.Timeout,
		Transport: transport,
	}
}

func baseProfiledTransport() *http.Transport {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if ok && baseTransport != nil {
		return baseTransport.Clone()
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
	if profile.MaxIdleConns > 0 {
		transport.MaxIdleConns = profile.MaxIdleConns
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
