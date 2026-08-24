package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewPublicHTTPHandlerPropagatesPatternToOriginalRequest(t *testing.T) {
	const pattern = "GET /items/{itemID}"

	for _, test := range []struct {
		name      string
		filter    func(*http.Request) bool
		wantSpans int
	}{
		{name: "traced", wantSpans: 1},
		{name: "filtered", filter: func(*http.Request) bool { return false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sdktrace.AlwaysSample()),
				sdktrace.WithSpanProcessor(recorder),
			)
			installPublicHTTPGlobals(t, provider, propagation.TraceContext{})

			handler := NewPublicHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Pattern = pattern

				w.WriteHeader(http.StatusNoContent)
			}), "public.pattern", HTTPHandlerOptions{
				Filter:           test.filter,
				SpanRoutePattern: true,
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", http.NoBody)

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if req.Pattern != pattern {
				t.Fatalf("original request Pattern = %q, want %q", req.Pattern, pattern)
			}

			if got := len(recorder.Ended()); got != test.wantSpans {
				t.Fatalf("ended spans = %d, want %d", got, test.wantSpans)
			}
		})
	}
}
