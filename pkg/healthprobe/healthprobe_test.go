package healthprobe

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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestCheckURLAcceptsSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := CheckURLInternal(server.URL); err != nil {
		t.Fatalf("CheckURLInternal(%q): %v", server.URL, err)
	}
}

func TestFetchURLReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, `{"mode":"active-active"}`)
	}))
	defer server.Close()

	body, err := FetchURLInternal(server.URL)
	if err != nil {
		t.Fatalf("FetchURLInternal(%q): %v", server.URL, err)
	}

	if string(body) != `{"mode":"active-active"}` {
		t.Fatalf("FetchURLInternal body = %q, want active-active json", body)
	}
}

func TestFetchURLWithHeadersSendsConfiguredHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != testProbeSecret {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		testsupport.WriteResponse(t, w, `ok`)
	}))
	defer server.Close()

	if _, err := FetchURLInternal(server.URL); err == nil {
		t.Fatal("FetchURLInternal() error = nil, want unauthorized without API key header")
	}

	body, err := FetchURLWithHeadersInternal(server.URL, map[string]string{"X-API-Key": testProbeSecret})
	if err != nil {
		t.Fatalf("FetchURLWithHeadersInternal(%q): %v", server.URL, err)
	}

	if string(body) != "ok" {
		t.Fatalf("FetchURLWithHeadersInternal body = %q, want ok", body)
	}
}

func TestFetchURLRejectsServerErrorWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)

		testsupport.WriteResponse(t, w, "boom")
	}))
	defer server.Close()

	if body, err := FetchURLInternal(server.URL); err == nil || body != nil {
		t.Fatalf("FetchURLInternal(%q) = (%q, %v), want nil body and error for 500", server.URL, body, err)
	}
}

func TestCheckURLRejectsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := CheckURLInternal(server.URL); err == nil {
		t.Fatalf("CheckURLInternal(%q) error = nil, want error for 500", server.URL)
	}
}

func TestCheckURLRejectsMissingCAFile(t *testing.T) {
	t.Setenv(CACertFileEnv, filepath.Join(t.TempDir(), "missing.pem"))

	if err := CheckURLInternal("https://127.0.0.1:1/ready"); err == nil {
		t.Fatal("CheckURLInternal() error = nil, want error for missing CA file")
	}
}

func TestCheckURLRejectsInvalidInputs(t *testing.T) {
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
			if err := CheckURL(tt.raw); err == nil {
				t.Fatalf("CheckURL(%q) error = nil, want error", tt.raw)
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

	listener, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer testsupport.CloseNow(t, "listener.Close", listener.Close)

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

	serveErr := make(chan error, 1)

	go func() { serveErr <- server.Serve(listener) }()

	defer func() {
		testsupport.CloseNow(t, "server.Close", server.Close)

		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve() error = %v", err)
		}
	}()

	t.Setenv(CACertFileEnv, certFile)
	t.Setenv(ServerNameEnv, "healthprobe-h3.local")

	url := "https://" + listener.LocalAddr().String() + "/ready"
	if err := CheckURLInternal(url); err != nil {
		t.Fatalf("CheckURLInternal(%q): %v", url, err)
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
