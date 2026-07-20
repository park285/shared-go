package h3

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
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

	server, err := NewServer(":0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), certFile, keyFile)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	client, closeFn, err := NewClient(5*time.Second, ClientOptions{
		CACertFile: certFile,
		ServerName: "h3-test.local",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer closeFn()

	resp, err := client.Get("https://" + listener.LocalAddr().String() + "/health")
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
