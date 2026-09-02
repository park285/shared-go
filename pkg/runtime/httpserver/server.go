package httpserver

import (
	"net/http"
	"time"

	"github.com/park285/shared-go/v2/pkg/telemetry"
)

// ServerOptions는 NewServer가 만드는 http.Server의 수신 한계와 tracing 설정이다.
type ServerOptions struct {
	// ReadHeaderTimeout은 요청 헤더 읽기 상한이다. 0이면 상한을 두지 않는다.
	ReadHeaderTimeout time.Duration
	// IdleTimeout은 keep-alive 연결의 유휴 상한이다. 0이면 http.Server 기본값을 따른다.
	IdleTimeout time.Duration
	// MaxHeaderBytes는 요청 헤더 최대 크기다. 0이면 http.Server 기본값을 따른다.
	MaxHeaderBytes int
	// EnableOTel은 OTelServiceName과 함께 설정될 때 OpenTelemetry HTTP 계측을 켠다.
	EnableOTel bool
	// OTelServiceName은 span 이름의 접두사이자 OTLP collector에 표시되는 서비스 이름이다.
	OTelServiceName string
}

// traceableRequest는 health·readiness·metrics 경로를 tracing에서 제외한다.
func traceableRequest(r *http.Request) bool {
	switch r.URL.Path {
	case "/health", "/ready", "/ready/runtime", "/metrics":
		return false
	default:
		return true
	}
}

// NewServer는 handler를 감싼 http.Server를 만든다.
//
// ReadTimeout은 두지 않는다. 본문 읽기 예산은 WithBodyReadTimeout이 handler 진입 시점부터
// 세므로 느린 헤더가 본문 예산을 갉아먹지 않는다. 빈 handler(nil)는 빈 ServeMux로 대체한다.
func NewServer(addr string, handler http.Handler, opts ServerOptions) *http.Server {
	if handler == nil {
		handler = http.NewServeMux()
	}

	finalHandler := handler

	if opts.EnableOTel && opts.OTelServiceName != "" {
		finalHandler = telemetry.NewPublicHTTPHandler(finalHandler, opts.OTelServiceName, telemetry.HTTPHandlerOptions{
			Filter:           traceableRequest,
			SpanRoutePattern: true,
		})
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           finalHandler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	if opts.IdleTimeout > 0 {
		server.IdleTimeout = opts.IdleTimeout
	}

	if opts.MaxHeaderBytes > 0 {
		server.MaxHeaderBytes = opts.MaxHeaderBytes
	}

	return server
}
