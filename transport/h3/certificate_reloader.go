package h3

import (
	"cmp"
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

// DefaultCertificateReloadInterval은 인증서 파일의 기본 갱신 확인 간격이다.
const DefaultCertificateReloadInterval = 30 * time.Second

// CertificateReloaderOptions는 갱신 확인 간격과 실패 진단 로거를 지정한다.
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
	failureMu sync.Mutex
	lastError string
}

// NewCertificateReloader는 초기 인증서 쌍을 검증하고 불변 snapshot을 준비한다.
// 인증서 쌍 파싱은 SAN, 신뢰 체인 또는 유효 수명 전체를 검증하지 않는다.
func NewCertificateReloader(certFile, keyFile string, opts CertificateReloaderOptions) (*CertificateReloader, error) {
	interval := opts.ReloadInterval
	if interval <= 0 {
		interval = DefaultCertificateReloadInterval
	}

	cert, fingerprint, err := loadCertificatePair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("h3: load certificate pair: %w", err)
	}

	r := &CertificateReloader{
		certFile:  certFile,
		keyFile:   keyFile,
		logger:    cmp.Or(opts.Logger, slog.Default()),
		interval:  interval,
		lastCheck: time.Now(),
	}
	r.current.Store(&certificateSnapshot{cert: cert, fingerprint: fingerprint})

	return r, nil
}

// Run은 취소될 때까지 인증서 갱신을 확인한다. 호출자가 goroutine의 join과 panic 처리를 소유한다.
func (r *CertificateReloader) Run(ctx context.Context) {
	if r == nil || ctx == nil {
		return
	}

	tick := time.Tick(r.interval)

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick:
			r.reloadIfDue(now)
		}
	}
}

// GetCertificate는 갱신을 확인하고 마지막 검증 성공 snapshot을 반환한다.
// 반환 인증서는 동시 handshake가 공유하므로 호출자가 변경하면 안 된다.
func (r *CertificateReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if r == nil {
		return nil, errors.New("h3: load certificate: nil reloader")
	}

	r.reloadIfDue(time.Now())

	snapshot := r.current.Load()
	if snapshot == nil || snapshot.cert == nil {
		return nil, errors.New("h3: load certificate: no cached certificate")
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
