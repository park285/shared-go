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
	// Overlay(Tailscale) MTU에서 fragmentation 없이 통과하는 보수값.
	initialPacketSize = 1200

	serverKeepAlivePeriod = 10 * time.Second
	serverMaxIdleTimeout  = 60 * time.Second

	// 죽은 피어에 묶이지 않도록 quic-go 기본(5s)보다 약간 관대한 핸드셰이크 상한.
	clientHandshakeIdleTimeout = 10 * time.Second
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
		Addr:           addr,
		Handler:        handler,
		TLSConfig:      http3.ConfigureTLSConfig(tlsConfig),
		QUICConfig:     newServerQUICConfig(),
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
	}
}

func newServerQUICConfig() *quic.Config {
	return &quic.Config{
		InitialPacketSize: initialPacketSize,
		KeepAlivePeriod:   serverKeepAlivePeriod,
		MaxIdleTimeout:    serverMaxIdleTimeout,
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
			return nil, nil, fmt.Errorf("load root c as: %w", err)
		}

		tlsConfig.RootCAs = roots
	}

	transport := &http3.Transport{
		TLSClientConfig: tlsConfig,
		QUICConfig:      newClientQUICConfig(),
	}

	if opts.DialGuard != nil {
		transport.Dial = newGuardedDialer(opts.DialGuard)
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	return client, func() {
		_ = transport.Close() //nolint:errcheck // 종료 시 transport 정리 실패는 무시
	}, nil
}

func newGuardedDialer(guard func(net.IP) error) func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
	return func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split h3 dial addr %s: %w", addr, err)
		}

		// net.ResolveUDPAddr는 ctx를 받지 않는다. transport.Close()는 mutex를 쥔 채
		// dial 취소를 기다리므로, 이름 해석이 ctx를 무시하면 종료와 그 client의 다른
		// 요청이 resolver 타임아웃 전부를 함께 기다린다.
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve h3 dial addr %s: %w", addr, err)
		}

		if len(addrs) == 0 {
			return nil, fmt.Errorf("resolve h3 dial addr %s: no addresses", addr)
		}

		chosen := preferIPv4(addrs)

		// net.IP의 판별 메서드는 nil/unspecified에서 전부 false라, 올바른 guard도
		// 이 주소를 허용한다. guard에 넘기기 전에 거른다.
		if len(chosen.IP) == 0 || chosen.IP.IsUnspecified() {
			return nil, fmt.Errorf("reject h3 dial addr %s: unusable destination address", addr)
		}

		if guardErr := guard(chosen.IP); guardErr != nil {
			return nil, fmt.Errorf("guard: %w", guardErr)
		}

		return quic.DialAddrEarly(ctx, net.JoinHostPort(dialHost(chosen), port), tlsCfg, cfg)
	}
}

// net.ResolveUDPAddr는 호스트명에 대해 addrList.first(isIPv4)로 IPv4를 우선 선택했다.
// LookupIPAddr은 그 필터도 RFC 6724 정렬도 적용하지 않으므로, 여기서 복원하지 않으면
// IPv6 egress가 없는 배포에서 AAAA를 골라 handshake 상한까지 매달린다.
func preferIPv4(addrs []net.IPAddr) net.IPAddr {
	for _, candidate := range addrs {
		if candidate.IP.To4() != nil {
			return candidate
		}
	}

	return addrs[0]
}

func dialHost(addr net.IPAddr) string {
	if addr.Zone == "" {
		return addr.IP.String()
	}

	return addr.IP.String() + "%" + addr.Zone
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
