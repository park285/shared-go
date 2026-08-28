package h3

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestNewClientUsesConstrainedInitialPacketSize(t *testing.T) {
	t.Parallel()

	client, closeFn, err := NewClient(0, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()

	transport, ok := client.Transport.(*http3.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http3.Transport", client.Transport)
	}

	if transport.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}

	if transport.QUICConfig.InitialPacketSize != initialPacketSize {
		t.Fatalf("InitialPacketSize = %d, want %d", transport.QUICConfig.InitialPacketSize, initialPacketSize)
	}
}

func TestNewClientQUICConfigMirrorsServerLiveness(t *testing.T) {
	t.Parallel()

	cfg := newClientQUICConfig()

	if cfg.InitialPacketSize != initialPacketSize {
		t.Errorf("InitialPacketSize = %d, want %d", cfg.InitialPacketSize, initialPacketSize)
	}

	if cfg.HandshakeIdleTimeout != clientHandshakeIdleTimeout {
		t.Errorf("HandshakeIdleTimeout = %s, want %s", cfg.HandshakeIdleTimeout, clientHandshakeIdleTimeout)
	}

	if cfg.MaxIdleTimeout != serverMaxIdleTimeout {
		t.Errorf("MaxIdleTimeout = %s, want %s (server symmetry)", cfg.MaxIdleTimeout, serverMaxIdleTimeout)
	}

	if cfg.KeepAlivePeriod != serverKeepAlivePeriod {
		t.Errorf("KeepAlivePeriod = %s, want %s (server symmetry)", cfg.KeepAlivePeriod, serverKeepAlivePeriod)
	}
}

func TestNewClientAppliesQUICConfig(t *testing.T) {
	t.Parallel()

	client, closeFn, err := NewClient(0, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()

	transport, ok := client.Transport.(*http3.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http3.Transport", client.Transport)
	}

	if transport.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}

	if transport.QUICConfig.KeepAlivePeriod != serverKeepAlivePeriod {
		t.Fatalf("KeepAlivePeriod = %s, want %s", transport.QUICConfig.KeepAlivePeriod, serverKeepAlivePeriod)
	}

	if transport.QUICConfig.MaxIdleTimeout != serverMaxIdleTimeout {
		t.Fatalf("MaxIdleTimeout = %s, want %s", transport.QUICConfig.MaxIdleTimeout, serverMaxIdleTimeout)
	}

	if transport.QUICConfig.HandshakeIdleTimeout != clientHandshakeIdleTimeout {
		t.Fatalf("HandshakeIdleTimeout = %s, want %s", transport.QUICConfig.HandshakeIdleTimeout, clientHandshakeIdleTimeout)
	}
}

func TestNewClientRejectsMissingCAFile(t *testing.T) {
	t.Parallel()

	_, _, err := NewClient(0, ClientOptions{CACertFile: filepath.Join(t.TempDir(), "missing.pem")})
	if err == nil {
		t.Fatal("NewClient() error = nil, want error for missing CA file")
	}
}

func TestNewClientRejectsIncompleteClientCertificatePair(t *testing.T) {
	t.Parallel()

	_, _, err := NewClient(0, ClientOptions{ClientCertFile: "client.crt"})
	if err == nil {
		t.Fatal("NewClient() error = nil, want incomplete client certificate rejection")
	}
}

func TestNewServerRejectsMissingCertPair(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := NewServer(":0", nil, filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
	if err == nil {
		t.Fatal("NewServer() error = nil, want error for missing cert pair")
	}
}

func TestNewServerWithTLSConfigZeroOptionsPreservesDefaultQUICLimits(t *testing.T) {
	t.Parallel()

	server := NewServerWithTLSConfig(":0", nil, &tls.Config{MinVersion: tls.VersionTLS13})
	if server.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}

	cfg := server.QUICConfig
	if cfg.MaxIncomingStreams != 0 {
		t.Fatalf("MaxIncomingStreams = %d, want 0 for quic-go default", cfg.MaxIncomingStreams)
	}

	if cfg.InitialStreamReceiveWindow != 0 {
		t.Fatalf("InitialStreamReceiveWindow = %d, want 0 for quic-go default", cfg.InitialStreamReceiveWindow)
	}
}

func TestServerClientLoopbackWithServerNameOverride(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, "h3-test.local")

	server, err := NewServer(":0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), certFile, keyFile)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	listener, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer testsupport.CloseNow(t, "listener.Close", listener.Close)

	serveErr := make(chan error, 1)

	go func() { serveErr <- server.Serve(listener) }()

	defer func() {
		testsupport.CloseNow(t, "server.Close", server.Close)

		if serveWaitErr := <-serveErr; serveWaitErr != nil && !errors.Is(serveWaitErr, http.ErrServerClosed) {
			t.Errorf("Serve() error = %v", serveWaitErr)
		}
	}()

	client, closeFn, err := NewClient(5*time.Second, ClientOptions{
		CACertFile: certFile,
		ServerName: "h3-test.local",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer closeFn()

	respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+listener.LocalAddr().String()+"/health", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(respReq)
	if err != nil {
		t.Fatalf("client.Get: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if resp.ProtoMajor != 3 {
		t.Fatalf("ProtoMajor = %d, want 3 (HTTP/3)", resp.ProtoMajor)
	}
}

func writeSelfSignedCert(t *testing.T, serverName string) (string, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: serverName},
		DNSNames:              []string{serverName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certFile, keyFile
}

func TestNewClientDialGuardRejectsUnusableDestination(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"https://0.0.0.0:44443/probe", "https://[::]:44443/probe"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			var (
				mu   sync.Mutex
				seen []net.IP
			)

			client, closeFn, err := NewClient(2*time.Second, ClientOptions{
				DialGuard: func(ip net.IP) error {
					mu.Lock()
					defer mu.Unlock()

					seen = append(seen, ip)

					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			defer closeFn()

			respReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}

			resp, err := client.Do(respReq)
			if err == nil {
				_ = resp.Body.Close()

				t.Fatalf("Get(%s) error = nil, want the dial rejected before the guard", target)
			}

			if !strings.Contains(err.Error(), "unusable destination address") {
				t.Fatalf("Get(%s) error = %v, want the unspecified-address rejection", target, err)
			}

			mu.Lock()
			defer mu.Unlock()

			if len(seen) != 0 {
				t.Fatalf("DialGuard saw %v, want unusable destinations rejected before the guard runs", seen)
			}
		})
	}
}

func TestPreferIPv4PreservesResolveUDPAddrSelection(t *testing.T) {
	t.Parallel()

	addresses := []net.IPAddr{
		{IP: net.ParseIP("2001:db8::1")},
		{IP: net.ParseIP("192.0.2.1")},
	}
	if got := preferIPv4(addresses); !got.IP.Equal(net.ParseIP("192.0.2.1")) {
		t.Fatalf("preferIPv4() = %v, want IPv4 candidate", got)
	}

	ipv6Only := []net.IPAddr{{IP: net.ParseIP("fe80::1"), Zone: "tailscale0"}}
	if got := preferIPv4(ipv6Only); !got.IP.Equal(ipv6Only[0].IP) || got.Zone != "tailscale0" {
		t.Fatalf("preferIPv4() = %v, want sole IPv6 candidate with zone", got)
	}
}

func TestDialHostPreservesIPv6Zone(t *testing.T) {
	t.Parallel()

	addr := net.IPAddr{IP: net.ParseIP("fe80::1"), Zone: "tailscale0"}
	if got := dialHost(addr); got != "fe80::1%tailscale0" {
		t.Fatalf("dialHost() = %q, want zone-qualified IPv6 host", got)
	}
}
