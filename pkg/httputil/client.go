package httputil

import (
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

// NewClient는 HTTP/1.1 전용 transport를 사용합니다. 같은 host로 동시 요청이 많은 소비자는
// NewInternalServiceClient 또는 NewExternalAPIClient를 사용해 더 큰 전용 pool을 확보하십시오.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: baseProfiledTransport()}
}

// proxy·keep-alive·pool baseline은 DefaultTransport에서 가져오고, HTTP는 HTTP/1.1만 사용합니다.
func NewProfiledClient(profile TransportProfile) *http.Client {
	transport := baseProfiledTransport()
	applyTransportProfile(transport, profile)

	return &http.Client{
		Timeout:   profile.Timeout,
		Transport: transport,
	}
}

func baseProfiledTransport() *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)

	transport := newHTTP1ProfiledTransport(protocols)
	copyDefaultTransportBaseline(transport)

	return transport
}

func newHTTP1ProfiledTransport(protocols *http.Protocols) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		Protocols:             protocols,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func copyDefaultTransportBaseline(transport *http.Transport) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || baseTransport == nil {
		return
	}

	transport.Proxy = baseTransport.Proxy
	transport.OnProxyConnectResponse = baseTransport.OnProxyConnectResponse
	transport.DialContext = baseTransport.DialContext
	transport.DisableKeepAlives = baseTransport.DisableKeepAlives
	transport.DisableCompression = baseTransport.DisableCompression
	transport.MaxIdleConns = baseTransport.MaxIdleConns
	transport.MaxIdleConnsPerHost = baseTransport.MaxIdleConnsPerHost
	transport.MaxConnsPerHost = baseTransport.MaxConnsPerHost
	transport.IdleConnTimeout = baseTransport.IdleConnTimeout
	transport.ResponseHeaderTimeout = baseTransport.ResponseHeaderTimeout
	transport.ExpectContinueTimeout = baseTransport.ExpectContinueTimeout
	transport.ProxyConnectHeader = baseTransport.ProxyConnectHeader.Clone()
	transport.GetProxyConnectHeader = baseTransport.GetProxyConnectHeader
	transport.MaxResponseHeaderBytes = baseTransport.MaxResponseHeaderBytes
	transport.WriteBufferSize = baseTransport.WriteBufferSize
	transport.ReadBufferSize = baseTransport.ReadBufferSize
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
