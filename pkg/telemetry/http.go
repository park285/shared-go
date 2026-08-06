package telemetry

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// HTTPHandlerOptions는 NewPublicHTTPHandler 설정입니다.
type HTTPHandlerOptions struct {
	// Filter는 원본 요청을 tracing할지 결정합니다.
	Filter           func(*http.Request) bool
	SpanRoutePattern bool
}

type httpHandlerRequestTargetKey struct{}

// NewPublicHTTPHandler는 개인정보를 span에 기록하지 않는 server tracing을 추가합니다.
func NewPublicHTTPHandler(handler http.Handler, operation string, opts HTTPHandlerOptions) http.Handler {
	if strings.TrimSpace(operation) == "" {
		return handler
	}

	restoredHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original, ok := r.Context().Value(httpHandlerRequestTargetKey{}).(*http.Request)
		if !ok {
			handler.ServeHTTP(w, r)
			return
		}

		restored := original.WithContext(r.Context())
		restored.Body = r.Body
		handler.ServeHTTP(w, restored)
		r.Pattern = restored.Pattern

		if opts.SpanRoutePattern && restored.Pattern != "" {
			trace.SpanFromContext(r.Context()).
				SetAttributes(semconv.HTTPRouteKey.String(restored.Pattern))
		}
	})

	// otelhttp는 handler 종료 후 r.Pattern이 있으면 이 formatter로 span 이름을 다시 쓴다.
	// 그래서 handler 안에서 SetName을 호출해도 여기서 덮인다 — 명명은 formatter에서만 한다.
	nameFormatter := func(operation string, r *http.Request) string {
		if opts.SpanRoutePattern && r != nil && r.Pattern != "" {
			return operation + " " + r.Pattern
		}

		return operation
	}

	instrumentedOptions := []otelhttp.Option{
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(nameFormatter),
	}

	if opts.Filter != nil {
		instrumentedOptions = append(instrumentedOptions, otelhttp.WithFilter(func(r *http.Request) bool {
			original, ok := r.Context().Value(httpHandlerRequestTargetKey{}).(*http.Request)
			if !ok {
				return opts.Filter(r)
			}

			return opts.Filter(original.WithContext(r.Context()))
		}))
	}

	instrumented := otelhttp.NewHandler(restoredHandler, operation, instrumentedOptions...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), httpHandlerRequestTargetKey{}, r)
		traced := r.WithContext(ctx)

		traced.Host = ""
		traced.RemoteAddr = ""
		traced.Header = r.Header.Clone()
		traced.Header.Del("Forwarded")
		traced.Header.Del("User-Agent")
		traced.Header.Del("X-Forwarded-For")
		traced.Header.Del("X-Real-IP")
		if traced.URL != nil {
			sanitizedURL := *traced.URL
			sanitizedURL.Scheme = ""
			sanitizedURL.Host = ""
			sanitizedURL.User = nil
			sanitizedURL.Path = ""
			sanitizedURL.RawPath = ""
			sanitizedURL.RawQuery = ""
			sanitizedURL.ForceQuery = false
			sanitizedURL.Fragment = ""
			sanitizedURL.RawFragment = ""
			sanitizedURL.Opaque = ""
			traced.URL = &sanitizedURL
		}
		traced.RequestURI = ""

		instrumented.ServeHTTP(w, traced)
		r.Pattern = traced.Pattern
	})
}
