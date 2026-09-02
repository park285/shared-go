package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewServerOTelIntegration(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installOTelGlobals(t, provider)

	const (
		serviceName   = "example-service"
		requestTarget = "/api/example/session/private-user-123?token=private-token-456"
	)

	var gotURL, gotRequestURI string

	server := NewServer(":0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RequestURI()
		gotRequestURI = r.RequestURI

		w.WriteHeader(http.StatusNoContent)
	}), ServerOptions{EnableOTel: true, OTelServiceName: serviceName})

	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, requestTarget, http.NoBody))

	if response.Code != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusNoContent)
	}

	if gotURL != requestTarget {
		t.Fatalf("handler URL = %q, want %q", gotURL, requestTarget)
	}

	if gotRequestURI != requestTarget {
		t.Fatalf("handler RequestURI = %q, want %q", gotRequestURI, requestTarget)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	if got := spans[0].Name(); got != serviceName {
		t.Fatalf("span name = %q, want %q", got, serviceName)
	}
}

func TestNewServerOTelGating(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installOTelGlobals(t, provider)

	for _, tc := range []struct {
		name string
		opts ServerOptions
	}{
		{name: "disabled", opts: ServerOptions{OTelServiceName: "example-service"}},
		{name: "missing service name", opts: ServerOptions{EnableOTel: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			server := NewServer(":0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true

				w.WriteHeader(http.StatusNoContent)
			}), tc.opts)

			server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody))

			if !called {
				t.Fatal("handler was not called")
			}
		})
	}

	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func TestNewServerAppliesServerOptions(t *testing.T) {
	const (
		addr              = "127.0.0.1:40258"
		readHeaderTimeout = 11 * time.Second
		idleTimeout       = 17 * time.Second
		maxHeaderBytes    = 8192
	)

	server := NewServer(addr, nil, ServerOptions{
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	})

	if server.Addr != addr {
		t.Fatalf("server address = %q, want %q", server.Addr, addr)
	}

	if server.Handler == nil {
		t.Fatal("server handler is nil")
	}

	if server.ReadHeaderTimeout != readHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, readHeaderTimeout)
	}

	if server.ReadTimeout != 0 {
		t.Fatalf("ReadTimeout = %s, want 0 because body budget starts at handler ingress", server.ReadTimeout)
	}

	if server.IdleTimeout != idleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, idleTimeout)
	}

	if server.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxHeaderBytes)
	}
}

func TestNewServerPreservesDisabledReadTimeouts(t *testing.T) {
	server := NewServer(":0", http.NotFoundHandler(), ServerOptions{})

	if server.ReadHeaderTimeout != 0 || server.ReadTimeout != 0 {
		t.Fatalf("disabled read timeouts = header %s body %s, want 0/0", server.ReadHeaderTimeout, server.ReadTimeout)
	}
}

func installOTelGlobals(t *testing.T, provider *sdktrace.TracerProvider) {
	t.Helper()

	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)

		if err := provider.Shutdown(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("provider.Shutdown: %v", err)
		}
	})
}

func TestNewServerSkipsHealthAndMetricsTraces(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	installOTelGlobals(t, provider)

	mux := http.NewServeMux()

	for _, pattern := range []string{"GET /health", "GET /metrics", "GET /api/example/items"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}

	server := NewServer(":0", mux, ServerOptions{EnableOTel: true, OTelServiceName: "example-service"})

	for _, target := range []string{"/health", "/metrics"} {
		server.Handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	}

	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans after health/metrics = %d, want 0", got)
	}

	server.Handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/example/items", http.NoBody))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	if got, want := spans[0].Name(), "example-service GET /api/example/items"; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
}
