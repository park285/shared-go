package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNewPublicHTTPHandlerRestoresRequestAndSanitizesSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	const (
		operation     = "public.http"
		requestTarget = "/api/private/private-user-123?token=private-token-456"
		remoteAddr    = "198.51.100.42:43123"
		forwardedFor  = "203.0.113.77, 198.51.100.42"
		userAgent     = "private-client/7.0"
		forwarded     = "for=203.0.113.77"
		realIP        = "203.0.113.77"
		host          = "private-tenant-123.example.com"
	)

	var (
		gotURL        *url.URL
		gotRequestURI string
		gotRemoteAddr string
		gotHost       string
		gotHeader     http.Header
		gotPattern    string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/private/{userID}", func(w http.ResponseWriter, r *http.Request) {
		copiedURL := *r.URL
		gotURL = &copiedURL
		gotRequestURI = r.RequestURI
		gotRemoteAddr = r.RemoteAddr
		gotHost = r.Host
		gotHeader = r.Header.Clone()
		gotPattern = r.Pattern
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, requestTarget, http.NoBody)
	req.RemoteAddr = remoteAddr
	req.Host = host
	req.Header.Set("X-Forwarded-For", forwardedFor)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Forwarded", forwarded)
	req.Header.Set("X-Real-IP", realIP)
	req.Header.Set("X-Private-Header", "private-header-value")
	req.URL.RawPath = "/api/private/private-user-%31%32%33"
	req.URL.ForceQuery = true
	req.URL.Fragment = "private-fragment-789"
	req.URL.RawFragment = "private-fragment-%37%38%39"
	req.URL.Opaque = "private-opaque-abc"
	wantURL := *req.URL
	wantRequestURI := req.RequestURI

	handler := NewPublicHTTPHandler(mux, operation, HTTPHandlerOptions{})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotURL == nil {
		t.Fatal("handler was not called")
	}
	if *gotURL != wantURL {
		t.Fatalf("handler URL = %#v, want %#v", *gotURL, wantURL)
	}
	if gotRequestURI != wantRequestURI {
		t.Fatalf("handler RequestURI = %q, want %q", gotRequestURI, wantRequestURI)
	}
	if gotRemoteAddr != remoteAddr {
		t.Fatalf("handler RemoteAddr = %q, want %q", gotRemoteAddr, remoteAddr)
	}
	if gotHost != host {
		t.Fatalf("handler Host = %q, want %q", gotHost, host)
	}
	for key, want := range map[string]string{
		"X-Forwarded-For":  forwardedFor,
		"User-Agent":       userAgent,
		"Forwarded":        forwarded,
		"X-Real-IP":        realIP,
		"X-Private-Header": "private-header-value",
	} {
		if got := gotHeader.Get(key); got != want {
			t.Fatalf("handler header %s = %q, want %q", key, got, want)
		}
	}
	if gotPattern != "GET /api/private/{userID}" {
		t.Fatalf("handler Pattern = %q, want %q", gotPattern, "GET /api/private/{userID}")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != operation {
		t.Fatalf("span name = %q, want %q", got, operation)
	}

	forbiddenKeys := map[attribute.Key]struct{}{
		attribute.Key("client.address"):       {},
		attribute.Key("network.peer.address"): {},
		attribute.Key("network.peer.port"):    {},
		attribute.Key("user_agent.original"):  {},
	}
	sentinels := []string{
		"private-user-123",
		"private-user-%31%32%33",
		"private-token-456",
		"private-fragment-789",
		"private-fragment-%37%38%39",
		"private-opaque-abc",
		remoteAddr,
		"198.51.100.42",
		forwardedFor,
		"203.0.113.77",
		userAgent,
		forwarded,
		realIP,
		host,
	}
	for _, attr := range spans[0].Attributes() {
		if _, forbidden := forbiddenKeys[attr.Key]; forbidden {
			t.Fatalf("forbidden client PII attribute %q recorded with value %q", attr.Key, attr.Value.String())
		}
		if attr.Key == attribute.Key("server.address") && attr.Value.String() != "" {
			t.Fatalf("untrusted host recorded in server.address with value %q", attr.Value.String())
		}
		for _, sentinel := range sentinels {
			if strings.Contains(attr.Value.String(), sentinel) {
				t.Fatalf("request identifier %q recorded in span attribute %q", sentinel, attr.Key)
			}
		}
	}
}

func TestNewPublicHTTPHandlerPreservesDynamicRequestTrailer(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	const (
		trailerName  = "X-Integrity-Digest"
		trailerValue = "sha256=private-digest-123"
		requestBody  = "body"
	)
	gotTrailer := make(chan string, 1)
	server := httptest.NewServer(NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotTrailer <- r.Trailer.Get(trailerName)
		w.WriteHeader(http.StatusNoContent)
	}), "public.trailer", HTTPHandlerOptions{}))
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Trailer = http.Header{trailerName: []string{trailerValue}}
	req.ContentLength = -1

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	resp.Body.Close()
	if got := <-gotTrailer; got != trailerValue {
		t.Fatalf("handler trailer = %q, want %q", got, trailerValue)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	for _, attr := range spans[0].Attributes() {
		if attr.Key == attribute.Key("http.request.body.size") {
			if got := attr.Value.AsInt64(); got != int64(len(requestBody)) {
				t.Fatalf("http.request.body.size = %d, want %d", got, len(requestBody))
			}
			return
		}
	}
	t.Fatal("http.request.body.size attribute is missing")
}

func TestNewPublicHTTPHandlerFilterObservesOriginalRequest(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	const requestTarget = "/health?probe=private-probe"
	var filtered *http.Request
	called := false
	handler := NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.URL.RequestURI(); got != requestTarget {
			t.Errorf("handler URL = %q, want %q", got, requestTarget)
		}
		w.WriteHeader(http.StatusNoContent)
	}), "public.filter", HTTPHandlerOptions{
		Filter: func(r *http.Request) bool {
			filtered = r.Clone(r.Context())
			return r.URL.Path == "/health" && r.URL.RawQuery == "probe=private-probe" &&
				r.RequestURI == requestTarget && r.RemoteAddr == "198.51.100.42:43123" &&
				r.Header.Get("User-Agent") == "private-client/7.0"
		},
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, requestTarget, http.NoBody)
	req.RemoteAddr = "198.51.100.42:43123"
	req.Header.Set("User-Agent", "private-client/7.0")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was not called")
	}
	if filtered == nil {
		t.Fatal("filter was not called")
	}
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("ended spans = %d, want 1", got)
	}
}

func TestNewPublicHTTPHandlerFilterCanRejectOriginalRequest(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	called := false
	handler := NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.URL.RequestURI(); got != "/metrics?probe=private" {
			t.Errorf("handler URL = %q, want %q", got, "/metrics?probe=private")
		}
		w.WriteHeader(http.StatusNoContent)
	}), "public.filter", HTTPHandlerOptions{
		Filter: func(r *http.Request) bool {
			return r.URL.Path != "/metrics"
		},
	})

	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics?probe=private", http.NoBody),
	)

	if !called {
		t.Fatal("handler was not called")
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func TestNewPublicHTTPHandlerRemoteSampledParentCannotOverrideLocalSampler(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithIDGenerator(publicHTTPUnsampledIDGenerator{}),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	var (
		called  bool
		sampled bool
	)
	handler := NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sampled = trace.SpanFromContext(r.Context()).SpanContext().IsSampled()
		w.WriteHeader(http.StatusNoContent)
	}), "public.sampling", HTTPHandlerOptions{})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private", http.NoBody)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was not called")
	}
	if sampled {
		t.Fatal("remote sampled traceparent overrode the local sampler")
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func TestNewPublicHTTPHandlerDoesNotExtractRemoteBaggage(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	var gotMember baggage.Member
	handler := NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMember = baggage.FromContext(r.Context()).Member("private-user")
		if got := r.Header.Get("baggage"); got != "private-user=private-user-123" {
			t.Errorf("handler baggage header = %q, want original header", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}), "public.baggage", HTTPHandlerOptions{})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private", http.NoBody)
	req.Header.Set("baggage", "private-user=private-user-123")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotMember.Key() != "" {
		t.Fatalf("remote baggage reached handler context: %q", gotMember.Key())
	}
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("ended spans = %d, want 1", got)
	}
}

func TestNewPublicHTTPHandlerBlankOperationReturnsOriginalHandler(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	original := &publicHTTPMarkerHandler{}
	filterCalled := false
	wrapped := NewPublicHTTPHandler(original, " \t\n ", HTTPHandlerOptions{
		Filter: func(*http.Request) bool {
			filterCalled = true
			return true
		},
	})
	if wrapped != original {
		t.Fatal("blank operation did not return the original handler")
	}

	wrapped.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/private", http.NoBody),
	)
	if filterCalled {
		t.Fatal("blank operation called the filter")
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

type publicHTTPMarkerHandler struct{}

func (*publicHTTPMarkerHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

type publicHTTPUnsampledIDGenerator struct{}

func (publicHTTPUnsampledIDGenerator) NewIDs(context.Context) (trace.TraceID, trace.SpanID) {
	return trace.TraceID{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}, trace.SpanID{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func (publicHTTPUnsampledIDGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return trace.SpanID{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func installPublicHTTPGlobals(t *testing.T, provider *sdktrace.TracerProvider, propagator propagation.TextMapPropagator) {
	t.Helper()

	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagator)

	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
}

func TestNewPublicHTTPHandlerRoutePatternNaming(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	const operation = "public.http"

	mux := http.NewServeMux()
	mux.HandleFunc("GET /rooms/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := NewPublicHTTPHandler(mux, operation, HTTPHandlerOptions{SpanRoutePattern: true})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/rooms/private-room-123", http.NoBody))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	const wantPattern = "GET /rooms/{id}"
	if got := spans[0].Name(); got != operation+" "+wantPattern {
		t.Fatalf("span name = %q, want %q", got, operation+" "+wantPattern)
	}

	var gotRoute string
	for _, attr := range spans[0].Attributes() {
		if attr.Key == attribute.Key("http.route") {
			gotRoute = attr.Value.AsString()
		}
		if attr.Key == attribute.Key("url.path") {
			t.Fatalf("url.path attribute must stay absent, got %q", attr.Value.AsString())
		}
	}
	if gotRoute != wantPattern {
		t.Fatalf("http.route = %q, want %q", gotRoute, wantPattern)
	}

	for _, attr := range spans[0].Attributes() {
		if strings.Contains(attr.Value.String(), "private-room-123") {
			t.Fatalf("attribute %s leaked the raw path segment", attr.Key)
		}
	}
}

func TestNewPublicHTTPHandlerRoutePatternUnmatchedKeepsOperation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	const operation = "public.http"

	handler := NewPublicHTTPHandler(http.NewServeMux(), operation, HTTPHandlerOptions{SpanRoutePattern: true})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/unregistered", http.NoBody))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != operation {
		t.Fatalf("span name = %q, want %q", got, operation)
	}
	for _, attr := range spans[0].Attributes() {
		if attr.Key == attribute.Key("http.route") {
			t.Fatalf("http.route must stay absent on an unmatched route, got %q", attr.Value.AsString())
		}
	}
}

func TestNewPublicHTTPHandlerRoutePatternDisabledByDefault(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

	const operation = "public.http"

	mux := http.NewServeMux()
	mux.HandleFunc("GET /rooms/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := NewPublicHTTPHandler(mux, operation, HTTPHandlerOptions{})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/rooms/private-room-123", http.NoBody))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Name(); got != operation {
		t.Fatalf("span name = %q, want %q", got, operation)
	}
}
