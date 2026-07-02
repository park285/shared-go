package h3

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
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

	// 죽은 피어에 묶이지 않도록 quic-go 기본(5s)보다 약간 관대한 핸드셰이크 상한.
	clientHandshakeIdleTimeout = 10 * time.Second
)

// ServerOptions는 HTTP/3 서버의 QUIC 수신 한도를 노출합니다.
// zero-value는 기존 quic-go 기본값을 그대로 사용합니다.
type ServerOptions struct {
	MaxIncomingStreams             int64
	MaxIncomingUniStreams          int64
	InitialStreamReceiveWindow     uint64
	MaxStreamReceiveWindow         uint64
	InitialConnectionReceiveWindow uint64
	MaxConnectionReceiveWindow     uint64
}

func NewServer(addr string, handler http.Handler, certFile, keyFile string) (*http3.Server, error) {
	return NewServerWithOptions(addr, handler, certFile, keyFile, ServerOptions{})
}

func NewServerWithOptions(addr string, handler http.Handler, certFile, keyFile string, opts ServerOptions) (*http3.Server, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate pair: %w", err)
	}

	return NewServerWithTLSConfigAndOptions(addr, handler, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}, opts), nil
}

func NewServerWithTLSConfig(addr string, handler http.Handler, tlsConfig *tls.Config) *http3.Server {
	return NewServerWithTLSConfigAndOptions(addr, handler, tlsConfig, ServerOptions{})
}

func NewServerWithTLSConfigAndOptions(addr string, handler http.Handler, tlsConfig *tls.Config, opts ServerOptions) *http3.Server {
	if handler == nil {
		handler = http.NotFoundHandler()
	}

	return &http3.Server{
		Addr:           addr,
		Handler:        handler,
		TLSConfig:      http3.ConfigureTLSConfig(tlsConfig),
		QUICConfig:     newServerQUICConfig(opts),
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
	}
}

func newServerQUICConfig(opts ServerOptions) *quic.Config {
	return &quic.Config{
		InitialPacketSize:              initialPacketSize,
		KeepAlivePeriod:                serverKeepAlivePeriod,
		MaxIdleTimeout:                 serverMaxIdleTimeout,
		MaxIncomingStreams:             opts.MaxIncomingStreams,
		MaxIncomingUniStreams:          opts.MaxIncomingUniStreams,
		InitialStreamReceiveWindow:     opts.InitialStreamReceiveWindow,
		MaxStreamReceiveWindow:         opts.MaxStreamReceiveWindow,
		InitialConnectionReceiveWindow: opts.InitialConnectionReceiveWindow,
		MaxConnectionReceiveWindow:     opts.MaxConnectionReceiveWindow,
	}
}

type ClientOptions struct {
	CACertFile string
	// SAN에 없는 주소(127.0.0.1, docker DNS)로 접속할 때 SAN에 있는 이름으로 검증한다.
	ServerName string
	DialGuard  func(net.IP) error
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
		QUICConfig:      newClientQUICConfig(),
	}
	if opts.DialGuard != nil {
		guard := opts.DialGuard
		transport.Dial = func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			udpAddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				return nil, fmt.Errorf("resolve h3 dial addr %s: %w", addr, err)
			}
			if guardErr := guard(udpAddr.IP); guardErr != nil {
				return nil, guardErr
			}
			return quic.DialAddrEarly(ctx, udpAddr.String(), tlsCfg, cfg)
		}
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	return client, func() {
		_ = transport.Close() //nolint:errcheck // 종료 시 transport 정리 실패는 무시
	}, nil
}

func newClientQUICConfig() *quic.Config {
	return &quic.Config{
		InitialPacketSize:    initialPacketSize,
		HandshakeIdleTimeout: clientHandshakeIdleTimeout,
		MaxIdleTimeout:       serverMaxIdleTimeout,
		KeepAlivePeriod:      serverKeepAlivePeriod,
	}
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
