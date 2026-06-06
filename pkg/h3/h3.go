package h3

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const (
	// overlay(Tailscale) MTU에서 fragmentation 없이 통과하는 보수값.
	initialPacketSize = 1200

	serverKeepAlivePeriod = 10 * time.Second
	serverMaxIdleTimeout  = 60 * time.Second
)

func NewServer(addr string, handler http.Handler, certFile, keyFile string) (*http3.Server, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate pair: %w", err)
	}

	return NewServerWithTLSConfig(addr, handler, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}), nil
}

func NewServerWithTLSConfig(addr string, handler http.Handler, tlsConfig *tls.Config) *http3.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}

	return &http3.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: http3.ConfigureTLSConfig(tlsConfig),
		QUICConfig: &quic.Config{
			InitialPacketSize: initialPacketSize,
			KeepAlivePeriod:   serverKeepAlivePeriod,
			MaxIdleTimeout:    serverMaxIdleTimeout,
		},
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
	}
}

type ClientOptions struct {
	CACertFile string
	// SAN에 없는 주소(127.0.0.1, docker DNS)로 접속할 때 SAN에 있는 이름으로 검증한다.
	ServerName string
}

func NewClient(timeout time.Duration, opts ClientOptions) (*http.Client, func(), error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: opts.ServerName,
	}

	if opts.CACertFile != "" {
		roots, err := loadRootCAs(opts.CACertFile)
		if err != nil {
			return nil, nil, err
		}
		tlsConfig.RootCAs = roots
	}

	transport := &http3.Transport{
		TLSClientConfig: tlsConfig,
		QUICConfig: &quic.Config{
			InitialPacketSize: initialPacketSize,
		},
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	return client, func() {
		_ = transport.Close() //nolint:errcheck // 종료 시 transport 정리 실패는 무시
	}, nil
}

func loadRootCAs(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path) //nolint:gosec // 운영자가 지정하는 CA 경로
	if err != nil {
		return nil, fmt.Errorf("read h3 CA file: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("read h3 CA file: no PEM certificates in %s", path)
	}

	return roots, nil
}
