package telemetry

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

// HTTPHandlerOptions는 NewPublicHTTPHandler 설정입니다.
type HTTPHandlerOptions struct {
	// Filter는 원본 요청을 tracing할지 결정합니다.
	Filter func(*http.Request) bool
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
	})

	instrumentedOptions := []otelhttp.Option{
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(func(operation string, _ *http.Request) string {
			return operation
		}),
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
