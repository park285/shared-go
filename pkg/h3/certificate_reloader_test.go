package h3

import (
	"crypto/tls"
	"encoding/pem"
	"os"
	"testing"
	"time"
)

func TestParseCertificatePoolAcceptsEveryValidCertificateBlock(t *testing.T) {
	firstFile, _ := writeSelfSignedCert(t, "first.test")
	secondFile, _ := writeSelfSignedCert(t, "second.test")
	first := mustReadFile(t, firstFile)
	second := mustReadFile(t, secondFile)

	pool, err := parseCertificatePool(append(append(first, '\n'), second...))
	if err != nil {
		t.Fatalf("parseCertificatePool() error = %v", err)
	}

	if got := len(pool.Subjects()); got != 2 {
		t.Fatalf("certificate subjects = %d, want 2", got)
	}
}

func TestParseCertificatePoolRejectsMalformedOrNonCertificateData(t *testing.T) {
	certFile, _ := writeSelfSignedCert(t, "strict.test")
	valid := mustReadFile(t, certFile)

	tests := []struct {
		name string
		pem  []byte
	}{
		{name: "trailing data", pem: append(append([]byte(nil), valid...), []byte("trailing")...)},
		{name: "leading data", pem: append([]byte("leading\n"), valid...)},
		{name: "non certificate", pem: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("invalid")})},
		{name: "invalid certificate", pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid")})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := parseCertificatePool(tc.pem)
			if err == nil {
				t.Fatalf("parseCertificatePool() = %v, want error", pool)
			}
		})
	}
}

func TestCertificateReloaderAppliesChangeAndKeepsCachedCertificateOnFailure(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, "reload.test")
	r, err := NewCertificateReloader(certFile, keyFile, CertificateReloaderOptions{ReloadInterval: time.Second})
	if err != nil {
		t.Fatalf("NewCertificateReloader() error = %v", err)
	}

	first, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("first GetCertificate() error = %v", err)
	}

	replacementCert, replacementKey := writeSelfSignedCert(t, "reload.test")
	mustWriteFile(t, certFile, mustReadFile(t, replacementCert))
	mustWriteFile(t, keyFile, mustReadFile(t, replacementKey))
	r.reloadIfDue(r.lastCheck.Add(r.interval))

	second, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("second GetCertificate() error = %v", err)
	}

	if second == first {
		t.Fatal("changed certificate pair did not replace cached snapshot")
	}

	mustWriteFile(t, keyFile, []byte("invalid private key"))
	r.reloadIfDue(r.lastCheck.Add(r.interval))

	third, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate() after reload failure error = %v", err)
	}

	if third != second {
		t.Fatal("reload failure replaced the last valid certificate")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-owned temporary path
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	return data
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
