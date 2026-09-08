package openaipreset_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

const completedJSONEvent = `{"type":"response.completed","response":{"id":"resp-1","status":"completed","model":"gpt-test","output":[{"type":"message","content":[{"type":"output_text","text":"{\"answer\":\"완료\"}"}]}],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}}}}`

func TestResponsesStreamCompletionBoundary(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		wantOK bool
	}{
		{name: "canonical output supersedes deltas", events: []string{`{"type":"response.output_text.delta","delta":"not canonical"}`, completedJSONEvent, "[DONE]"}, wantOK: true},
		{name: "normal EOF after completion", events: []string{completedJSONEvent}, wantOK: true},
		{name: "delta only", events: []string{`{"type":"response.output_text.delta","delta":"partial"}`, "[DONE]"}},
		{name: "empty stream"},
		{name: "failed", events: []string{`{"type":"response.failed","response":{"status":"failed"}}`}},
		{name: "incomplete", events: []string{`{"type":"response.incomplete","response":{"status":"incomplete"}}`}},
		{name: "error", events: []string{`{"type":"error","message":"private-provider-marker"}`}},
		{name: "SDK error", events: []string{`{"error":{"message":"private-provider-marker"}}`}},
		{name: "invalid status", events: []string{`{"type":"response.completed","response":{"id":"resp-1","status":"incomplete"}}`}},
		{name: "missing response", events: []string{`{"type":"response.completed"}`}},
		{name: "duplicate completion", events: []string{completedJSONEvent, completedJSONEvent}},
		{name: "late delta", events: []string{completedJSONEvent, `{"type":"response.output_text.delta","delta":"late"}`}},
		{name: "late parse error", events: []string{completedJSONEvent, `{"private-provider-marker"`}},
		{name: "late SDK error", events: []string{completedJSONEvent, `{"error":{"message":"private-provider-marker"}}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")

				for _, event := range tt.events {
					if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
						t.Errorf("write event: %v", err)

						return
					}
				}
			}))

			defer server.Close()

			reporter := &recordingReporter{}

			client, err := openaipreset.New(server.URL, "test-key", "gpt-test", openaipreset.WithUsageReporter(reporter))
			if err != nil {
				t.Fatal(err)
			}

			got, err := client.GenerateLayeredResponsesJSON(t.Context(), "task", openaipreset.PromptLayers{User: "question"}, map[string]any{"type": "object"})
			if tt.wantOK {
				assertCompletedStream(t, got, err, reporter)
			} else if err == nil || got != "" || reporter.called {
				t.Fatalf("failed stream returned output = %q, error = %v, usage = %v", got, err, reporter.called)
			}

			if err != nil && strings.Contains(err.Error(), "private-provider-marker") {
				t.Fatal("provider error payload leaked")
			}

			if calls.Load() != 1 {
				t.Fatalf("calls = %d, want 1", calls.Load())
			}
		})
	}
}

func assertCompletedStream(t *testing.T, got string, err error, reporter *recordingReporter) {
	t.Helper()

	if err != nil || got != `{"answer":"완료"}` {
		t.Fatalf("output = %q, error = %v", got, err)
	}

	if !reporter.called || reporter.usage.TotalTokens != 10 || reporter.usage.CachedInputTokens != 2 || reporter.usage.ReasoningOutputTokens != 1 {
		t.Fatalf("usage = %+v, called = %v", reporter.usage, reporter.called)
	}
}

func TestResponsesStreamCancellationDiscardsPartialOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		if _, err := fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"); err != nil {
			t.Errorf("write delta: %v", err)

			return
		}

		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush delta: %v", err)

			return
		}

		cancel()
		<-r.Context().Done()
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.GenerateLayeredResponsesJSON(ctx, "task", openaipreset.PromptLayers{User: "question"}, map[string]any{"type": "object"})
	if got != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("output = %q, error = %v, want cancellation without output", got, err)
	}
}

func writeResponsesCompleted(t *testing.T, w http.ResponseWriter, response string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")

	if _, err := fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\ndata: [DONE]\n\n", response); err != nil {
		t.Fatalf("write response stream: %v", err)
	}
}
