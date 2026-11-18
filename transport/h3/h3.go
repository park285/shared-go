package h3

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

const (
	initialPacketSize = 1200

	serverKeepAlivePeriod = 10 * time.Second
	serverMaxIdleTimeout  = 60 * time.Second

	clientHandshakeIdleTimeout = 10 * time.Second
)

func NewServer(addr string, handler http.Handler, certFile, keyFile string) (*http3.Server, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("h3: load certificate pair: %w", err)
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
	CACertFile     string
	ClientCertFile string
	ClientKeyFile  string
	ServerName     string
	DialGuard      func(net.IP) error
}

func NewClient(timeout time.Duration, opts ClientOptions) (*http.Client, func(), error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: opts.ServerName,
	}

	if opts.CACertFile != "" {
		roots, err := loadRootCAs(opts.CACertFile)
		if err != nil {
			return nil, nil, fmt.Errorf("h3: %w", err)
		}

		tlsConfig.RootCAs = roots
	}

	if (opts.ClientCertFile == "") != (opts.ClientKeyFile == "") {
		return nil, nil, errors.New("h3: client certificate and key must be configured together")
	}

	if opts.ClientCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(opts.ClientCertFile, opts.ClientKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("h3: load client certificate pair: %w", err)
		}

		tlsConfig.Certificates = []tls.Certificate{certificate}
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
			return nil, fmt.Errorf("h3: split dial addr %s: %w", addr, err)
		}

		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("h3: resolve dial addr %s: %w", addr, err)
		}

		if len(addrs) == 0 {
			return nil, fmt.Errorf("h3: resolve dial addr %s: no addresses", addr)
		}

		chosen := preferIPv4(addrs)

		if len(chosen.IP) == 0 || chosen.IP.IsUnspecified() {
			return nil, fmt.Errorf("h3: reject dial addr %s: unusable destination address", addr)
		}

		if guardErr := guard(chosen.IP); guardErr != nil {
			return nil, fmt.Errorf("h3: guard dial addr %s: %w", addr, guardErr)
		}

		conn, err := quic.DialAddrEarly(ctx, net.JoinHostPort(dialHost(chosen), port), tlsCfg, cfg)
		if err != nil {
			return nil, fmt.Errorf("h3: dial %s: %w", addr, err)
		}

		return conn, nil
	}
}

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
		return nil, fmt.Errorf("load root CAs: read h3 CA file: %w", err)
	}

	roots, err := parseCertificatePool(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("load root CAs: parse h3 CA pool: %w", err)
	}

	return roots, nil
}

func parseCertificatePool(pemBytes []byte) (*x509.CertPool, error) {
	roots := x509.NewCertPool()
	remaining := pemBytes
	count := 0

	for len(bytes.TrimSpace(remaining)) > 0 {
		trimmed := bytes.TrimSpace(remaining)
		if !bytes.HasPrefix(trimmed, []byte("-----BEGIN ")) {
			return nil, errors.New("parse h3 CA file: non-PEM data")
		}

		block, rest := pem.Decode(trimmed)
		if block == nil {
			return nil, errors.New("parse h3 CA file: malformed PEM block")
		}

		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errors.New("parse h3 CA file: unexpected PEM block")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, errors.New("parse h3 CA file: invalid certificate")
		}

		roots.AddCert(cert)

		count++

		remaining = rest
	}

	if count == 0 {
		return nil, errors.New("parse h3 CA file: no certificates")
	}

	return roots, nil
}
