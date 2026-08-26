package h3

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultCertificateReloadInterval = 30 * time.Second

type CertificateReloaderOptions struct {
	ReloadInterval time.Duration
	Logger         *slog.Logger
}

type certificateFingerprint struct {
	certPEM [sha256.Size]byte
	keyPEM  [sha256.Size]byte
}

type certificateSnapshot struct {
	cert        *tls.Certificate
	fingerprint certificateFingerprint
}

// CertificateReloader는 TLS 인증서 쌍의 검증된 snapshot을 원자적으로 제공한다.
// 갱신 실패 시 마지막 검증 성공 snapshot을 유지하고 동일 오류 진단은 중복 기록하지 않는다.
type CertificateReloader struct {
	certFile string
	keyFile  string
	logger   *slog.Logger
	interval time.Duration

	current   atomic.Pointer[certificateSnapshot]
	reloadMu  sync.Mutex
	lastCheck time.Time
	startOnce sync.Once
	failureMu sync.Mutex
	lastError string
}

func NewCertificateReloader(certFile, keyFile string, opts CertificateReloaderOptions) (*CertificateReloader, error) {
	interval := opts.ReloadInterval
	if interval <= 0 {
		interval = DefaultCertificateReloadInterval
	}

	cert, fingerprint, err := loadCertificatePair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate pair: %w", err)
	}

	r := &CertificateReloader{
		certFile:  certFile,
		keyFile:   keyFile,
		logger:    opts.Logger,
		interval:  interval,
		lastCheck: time.Now(),
	}
	r.current.Store(&certificateSnapshot{cert: cert, fingerprint: fingerprint})

	return r, nil
}

// Start는 ctx 수명 동안 백그라운드 갱신을 시작한다. 여러 호출 중 첫 호출만 유효하다.
func (r *CertificateReloader) Start(ctx context.Context) {
	if r == nil || ctx == nil {
		return
	}

	r.startOnce.Do(func() {
		go r.run(ctx)
	})
}

func (r *CertificateReloader) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.reloadIfDue(now)
		}
	}
}

// GetCertificate는 handshake 경로에서 파일을 매번 읽지 않는다. Start를 사용하지 않는
// 소비자도 interval이 지난 첫 handshake에서 갱신을 시도한다.
func (r *CertificateReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if r == nil {
		return nil, errors.New("load h3 certificate: nil reloader")
	}

	r.reloadIfDue(time.Now())

	snapshot := r.current.Load()
	if snapshot == nil || snapshot.cert == nil {
		return nil, errors.New("load h3 certificate: no cached certificate")
	}

	return snapshot.cert, nil
}

func (r *CertificateReloader) reloadIfDue(now time.Time) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	if now.Sub(r.lastCheck) < r.interval {
		return
	}

	r.lastCheck = now

	cert, fingerprint, err := loadCertificatePair(r.certFile, r.keyFile)
	if err != nil {
		r.recordFailure(err)

		return
	}

	snapshot := r.current.Load()
	if snapshot == nil || snapshot.fingerprint != fingerprint {
		r.current.Store(&certificateSnapshot{cert: cert, fingerprint: fingerprint})
	}

	r.clearFailure()
}

func loadCertificatePair(certFile, keyFile string) (*tls.Certificate, certificateFingerprint, error) {
	var fingerprint certificateFingerprint

	certPEM, err := os.ReadFile(certFile) //nolint:gosec // 운영자가 지정하는 인증서 경로
	if err != nil {
		return nil, fingerprint, fmt.Errorf("read h3 certificate file: %w", err)
	}

	keyPEM, err := os.ReadFile(keyFile) //nolint:gosec // 운영자가 지정하는 개인키 경로
	if err != nil {
		return nil, fingerprint, fmt.Errorf("read h3 key file: %w", err)
	}

	fingerprint = certificateFingerprint{
		certPEM: sha256.Sum256(certPEM),
		keyPEM:  sha256.Sum256(keyPEM),
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fingerprint, fmt.Errorf("parse h3 certificate pair: %w", err)
	}

	return &cert, fingerprint, nil
}

func (r *CertificateReloader) recordFailure(err error) {
	if r.logger == nil {
		return
	}

	r.failureMu.Lock()
	defer r.failureMu.Unlock()

	message := err.Error()
	if message == r.lastError {
		return
	}

	r.lastError = message
	r.logger.Warn("h3 certificate reload failed; using previous certificate", "error", err)
}

func (r *CertificateReloader) clearFailure() {
	r.failureMu.Lock()
	defer r.failureMu.Unlock()

	r.lastError = ""
}
