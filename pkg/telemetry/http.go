package telemetry

import (
	"context"
	"net/http"
	"net/url"
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

type httpHandlerRequestState struct {
	original *http.Request
	pattern  string
}

// NewPublicHTTPHandler는 개인정보를 span에 기록하지 않는 server tracing을 추가합니다.
func NewPublicHTTPHandler(handler http.Handler, operation string, opts HTTPHandlerOptions) http.Handler {
	if strings.TrimSpace(operation) == "" {
		return handler
	}

	instrumented := otelhttp.NewHandler(
		newPublicHTTPRestoringHandler(handler, opts.SpanRoutePattern),
		operation,
		publicHTTPTraceOptions(opts)...,
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &httpHandlerRequestState{original: r}
		traced := sanitizedPublicHTTPRequest(r, state)

		instrumented.ServeHTTP(w, traced)

		r.Pattern = state.pattern
	})
}

func newPublicHTTPRestoringHandler(handler http.Handler, spanRoutePattern bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := publicHTTPRequestState(r.Context())
		if state == nil {
			handler.ServeHTTP(w, r)

			return
		}

		restored := state.original.WithContext(r.Context())

		restored.Body = r.Body
		handler.ServeHTTP(w, restored)

		state.pattern = restored.Pattern
		r.Pattern = restored.Pattern

		if spanRoutePattern && restored.Pattern != "" {
			trace.SpanFromContext(r.Context()).
				SetAttributes(semconv.HTTPRouteKey.String(restored.Pattern))
		}
	})
}

func publicHTTPTraceOptions(opts HTTPHandlerOptions) []otelhttp.Option {
	options := []otelhttp.Option{
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(publicHTTPSpanNameFormatter(opts.SpanRoutePattern)),
	}

	if opts.Filter != nil {
		options = append(options, otelhttp.WithFilter(publicHTTPFilter(opts.Filter)))
	}

	return options
}

func publicHTTPSpanNameFormatter(spanRoutePattern bool) func(string, *http.Request) string {
	// otelhttp는 handler 종료 후 r.Pattern이 있으면 이 formatter로 span 이름을 다시 쓴다.
	// 그래서 handler 안에서 SetName을 호출해도 여기서 덮인다 — 명명은 formatter에서만 한다.
	return func(operation string, r *http.Request) string {
		if spanRoutePattern && r != nil && r.Pattern != "" {
			return operation + " " + r.Pattern
		}

		return operation
	}
}

func publicHTTPFilter(filter func(*http.Request) bool) func(*http.Request) bool {
	return func(r *http.Request) bool {
		state := publicHTTPRequestState(r.Context())
		if state == nil {
			return filter(r)
		}

		return filter(state.original.WithContext(r.Context()))
	}
}

func publicHTTPRequestState(ctx context.Context) *httpHandlerRequestState {
	state, ok := ctx.Value(httpHandlerRequestTargetKey{}).(*httpHandlerRequestState)
	if !ok || state == nil || state.original == nil {
		return nil
	}

	return state
}

func sanitizedPublicHTTPRequest(r *http.Request, state *httpHandlerRequestState) *http.Request {
	traced := r.WithContext(context.WithValue(r.Context(), httpHandlerRequestTargetKey{}, state))

	traced.Host = ""
	traced.RemoteAddr = ""
	traced.Header = r.Header.Clone()
	traced.Header.Del("Forwarded")
	traced.Header.Del("User-Agent")
	traced.Header.Del("X-Forwarded-For")
	traced.Header.Del("X-Real-IP")

	traced.URL = sanitizedPublicHTTPURL(traced.URL)
	traced.RequestURI = ""

	return traced
}

func sanitizedPublicHTTPURL(original *url.URL) *url.URL {
	if original == nil {
		return nil
	}

	sanitized := *original

	sanitized.Scheme = ""
	sanitized.Host = ""
	sanitized.User = nil
	sanitized.Path = ""
	sanitized.RawPath = ""
	sanitized.RawQuery = ""
	sanitized.ForceQuery = false
	sanitized.Fragment = ""
	sanitized.RawFragment = ""
	sanitized.Opaque = ""

	return &sanitized
}
