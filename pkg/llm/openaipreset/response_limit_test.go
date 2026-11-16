package openaipreset_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/httputil"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

type closeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)

	return nil
}

type staticResponseTransport struct {
	statusCode    int
	contentType   string
	body          *closeTrackingBody
	contentLength int64
}

func (t *staticResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Content-Type", t.contentType)

	return &http.Response{
		StatusCode:    t.statusCode,
		Status:        fmt.Sprintf("%d %s", t.statusCode, http.StatusText(t.statusCode)),
		Header:        header,
		Body:          t.body,
		ContentLength: t.contentLength,
		Request:       req,
	}, nil
}

func newStaticResponseClient(t *testing.T, statusCode int, contentType, body string, contentLength int64, opts ...openaipreset.Option) (*openaipreset.Client, *closeTrackingBody) {
	t.Helper()

	trackedBody := &closeTrackingBody{Reader: strings.NewReader(body)}
	transport := &staticResponseTransport{
		statusCode:    statusCode,
		contentType:   contentType,
		body:          trackedBody,
		contentLength: contentLength,
	}
	httpClient := &http.Client{Transport: transport}
	allOpts := append([]openaipreset.Option{openaipreset.WithMaxRetries(0), openaipreset.WithHTTPClient(httpClient)}, opts...)

	client, err := openaipreset.New("https://provider.invalid", "test-key", "gpt-test", allOpts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if httpClient.Transport != transport {
		t.Fatal("New mutated the caller's HTTP transport")
	}

	return client, trackedBody
}

func TestCompleteRejectsOversizedProviderResponse(t *testing.T) {
	reporter := &recordingReporter{}
	responseBody := `{"id":"resp-large","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"` + strings.Repeat("x", 1<<20) + `","annotations":[]}]}]}`
	client, body := newStaticResponseClient(t, http.StatusOK, "application/json", responseBody, -1, openaipreset.WithUsageReporter(reporter))

	got, err := client.Complete(t.Context(), openaipreset.CompletionRequest{
		Messages: []sharedllm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("Complete error = %v, want ErrResponseBodyTooLarge", err)
	}

	if got != (openaipreset.CompletionResponse{}) {
		t.Fatalf("Complete response = %+v, want zero value", got)
	}

	if reporter.called {
		t.Fatalf("usage recorded for oversized response: %+v", reporter)
	}

	if !body.closed.Load() {
		t.Fatal("oversized provider response body was not closed")
	}
}

func TestCompleteRejectsOversizedProviderError(t *testing.T) {
	const privateMarker = "PRIVATE_OVERSIZED_PROVIDER_MARKER"

	responseBody := `{"error":{"message":"` + privateMarker + strings.Repeat("x", 1<<20) + `","type":"server_error"}}`
	client, body := newStaticResponseClient(t, http.StatusInternalServerError, "application/json", responseBody, int64(len(responseBody)))

	_, err := client.Complete(t.Context(), openaipreset.CompletionRequest{
		Messages: []sharedllm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("Complete error = %v, want ErrResponseBodyTooLarge", err)
	}

	if strings.Contains(err.Error(), privateMarker) {
		t.Fatalf("oversized provider error leaked response content: %v", err)
	}

	if !body.closed.Load() {
		t.Fatal("oversized provider error body was not closed")
	}
}

func TestResponsesStreamRejectsCumulativeOversize(t *testing.T) {
	var responseBody strings.Builder

	delta := strings.Repeat("x", 32<<10)
	for responseBody.Len() <= 1<<20 {
		responseBody.WriteString(`data: {"type":"response.output_text.delta","delta":"`)
		responseBody.WriteString(delta)
		responseBody.WriteString("\"}\n\n")
	}

	responseBody.WriteString("data: ")
	responseBody.WriteString(completedJSONEvent)
	responseBody.WriteString("\n\n")

	reporter := &recordingReporter{}
	client, body := newStaticResponseClient(t, http.StatusOK, "text/event-stream", responseBody.String(), -1, openaipreset.WithUsageReporter(reporter))

	got, err := client.GenerateLayeredResponsesJSON(t.Context(), "task", openaipreset.PromptLayers{User: "question"}, map[string]any{"type": "object"})
	if !errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		t.Fatalf("GenerateLayeredResponsesJSON error = %v, want ErrResponseBodyTooLarge", err)
	}

	if got != "" {
		t.Fatalf("GenerateLayeredResponsesJSON output = %q, want empty", got)
	}

	if reporter.called {
		t.Fatalf("usage recorded for oversized stream: %+v", reporter)
	}

	if !body.closed.Load() {
		t.Fatal("oversized provider stream was not closed")
	}
}
