package healthprobe

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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func TestCheckURLAcceptsSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := CheckURL(server.URL); err != nil {
		t.Fatalf("CheckURL(%q): %v", server.URL, err)
	}
}

func TestFetchURLReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"mode":"active-active"}`))
	}))
	defer server.Close()

	body, err := FetchURL(server.URL)
	if err != nil {
		t.Fatalf("FetchURL(%q): %v", server.URL, err)
	}
	if string(body) != `{"mode":"active-active"}` {
		t.Fatalf("FetchURL body = %q, want active-active json", body)
	}
}

func TestFetchURLWithHeadersSendsConfiguredHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "probe-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	if _, err := FetchURL(server.URL); err == nil {
		t.Fatal("FetchURL() error = nil, want unauthorized without API key header")
	}

	body, err := FetchURLWithHeaders(server.URL, map[string]string{"X-API-Key": "probe-secret"})
	if err != nil {
		t.Fatalf("FetchURLWithHeaders(%q): %v", server.URL, err)
	}
	if string(body) != "ok" {
		t.Fatalf("FetchURLWithHeaders body = %q, want ok", body)
	}
}

func TestFetchURLRejectsServerErrorWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	if body, err := FetchURL(server.URL); err == nil || body != nil {
		t.Fatalf("FetchURL(%q) = (%q, %v), want nil body and error for 500", server.URL, body, err)
	}
}

func TestCheckURLRejectsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := CheckURL(server.URL); err == nil {
		t.Fatalf("CheckURL(%q) error = nil, want error for 500", server.URL)
	}
}

func TestCheckURLRejectsMissingCAFile(t *testing.T) {
	t.Setenv(CACertFileEnv, filepath.Join(t.TempDir(), "missing.pem"))

	if err := CheckURL("https://127.0.0.1:1/ready"); err == nil {
		t.Fatal("CheckURL() error = nil, want error for missing CA file")
	}
}

func TestParseURLRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing scheme", raw: "localhost:30001/ready"},
		{name: "unsupported scheme", raw: "ftp://localhost/ready"},
		{name: "missing host", raw: "http:///ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseURL(tt.raw); err == nil {
				t.Fatalf("ParseURL(%q) error = nil, want error", tt.raw)
			}
		})
	}
}

func TestCheckURLAcceptsHTTP3LoopbackWithServerNameOverride(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, "healthprobe-h3.local")
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load cert: %v", err)
	}

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	server := &http3.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ready" {
				t.Errorf("path = %q, want /ready", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		},
	}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	t.Setenv(CACertFileEnv, certFile)
	t.Setenv(ServerNameEnv, "healthprobe-h3.local")

	url := "https://" + listener.LocalAddr().String() + "/ready"
	if err := CheckURL(url); err != nil {
		t.Fatalf("CheckURL(%q): %v", url, err)
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
