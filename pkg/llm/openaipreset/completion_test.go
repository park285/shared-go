package openaipreset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/llm/openaipreset"
)

type contextBlockingTransport struct {
	started chan struct{}
	once    sync.Once
}

func (t *contextBlockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() { close(t.started) })
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestCompleteResponsesRequest(t *testing.T) {
	t.Parallel()

	temp := 1.0
	cases := []struct {
		name  string
		req   openaipreset.CompletionRequest
		check func(t *testing.T, payload map[string]any)
	}{
		{
			name: "basic",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: "system", Content: " system prompt "},
					{Role: "assistant", Content: "previous"},
					{Role: "user", Content: "   "},
					{Role: "user", Content: " hello "},
				},
			},
			check: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if got := payload["model"]; got != "gpt-test" {
					t.Fatalf("model = %#v, want gpt-test", got)
				}
				input := payload["input"].([]any)
				if len(input) != 3 {
					t.Fatalf("input len = %d, want 3", len(input))
				}
				assertMessagePayload(t, input[0], "system", "system prompt")
				assertMessagePayload(t, input[1], "assistant", "previous")
				assertMessagePayload(t, input[2], "user", "hello")
				if _, ok := payload["temperature"]; ok {
					t.Fatalf("temperature = %#v, want omitted", payload["temperature"])
				}
			},
		},
		{
			name: "all options",
			req: openaipreset.CompletionRequest{
				Messages:        []openaipreset.Message{{Role: "developer", Content: "stay terse"}},
				Model:           " gpt-override ",
				Temperature:     &temp,
				ReasoningEffort: " high ",
				WebSearch:       true,
				CacheKey:        " cache-1 ",
				ResponseFormat: &openaipreset.ResponseFormat{
					Name:   " answer_schema ",
					Schema: map[string]any{"type": "object"},
					Strict: true,
				},
			},
			check: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if got := payload["model"]; got != "gpt-override" {
					t.Fatalf("model = %#v, want gpt-override", got)
				}
				if got := payload["temperature"]; got != 1.0 {
					t.Fatalf("temperature = %#v, want 1", got)
				}
				if got := payload["prompt_cache_key"]; got != "cache-1" {
					t.Fatalf("prompt_cache_key = %#v, want cache-1", got)
				}
				if got := payload["tool_choice"]; got != "auto" {
					t.Fatalf("tool_choice = %#v, want auto", got)
				}
				assertJSONContains(t, payload["tools"], "web_search")
				assertJSONContains(t, payload["reasoning"], "high")
				assertCompletionSchema(t, payload["text"])
				input := payload["input"].([]any)
				assertMessagePayload(t, input[0], "user", "stay terse")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var payload map[string]any
			reporter := &recordingReporter{}
			var logs bytes.Buffer
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/responses" {
					t.Fatalf("path = %s, want /responses", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Fatalf("authorization = %q, want bearer test-key", got)
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				writeJSON(t, w, responsesBody)
			}))
			defer server.Close()

			client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
				openaipreset.WithUsageReporter(reporter),
				openaipreset.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))),
			)
			if err != nil {
				t.Fatalf("New error = %v", err)
			}

			got, err := client.Complete(t.Context(), tc.req)
			if err != nil {
				t.Fatalf("Complete error = %v", err)
			}
			if got.Text != `{"answer":"yes"}` {
				t.Fatalf("Text = %q, want response text", got.Text)
			}
			if got.Model != "gpt-returned" {
				t.Fatalf("Model = %q, want gpt-returned", got.Model)
			}
			if got.Usage != (sharedllm.Usage{InputTokens: 12, OutputTokens: 5, TotalTokens: 17, CachedInputTokens: 2, ReasoningOutputTokens: 1}) {
				t.Fatalf("Usage = %+v", got.Usage)
			}
			if !reporter.called || reporter.provider != "openai" || reporter.model != "gpt-returned" || reporter.usage != got.Usage {
				t.Fatalf("reporter = %+v, want successful completion usage", reporter)
			}
			for _, event := range []string{"llm.request.started", "llm.request.succeeded"} {
				if !strings.Contains(logs.String(), event) {
					t.Fatalf("logs missing %q: %s", event, logs.String())
				}
			}
			tc.check(t, payload)
		})
	}
}

func TestCompleteRejectsRefusalAndEmptyToolEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{
			name:    "refusal",
			body:    `{"id":"resp-refusal","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"private policy refusal"}]}]}`,
			wantErr: sharedllm.ErrOpenAIRefusalOutput,
		},
		{
			name:    "tool envelope",
			body:    `{"id":"resp-tool","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"tool_calls\":[]}","annotations":[]}]}]}`,
			wantErr: sharedllm.ErrOpenAIEmptyOutput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reporter := &recordingReporter{}
			var logs bytes.Buffer
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, tt.body)
			}))
			defer server.Close()

			client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
				openaipreset.WithUsageReporter(reporter),
				openaipreset.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))),
			)
			if err != nil {
				t.Fatalf("New error = %v", err)
			}

			_, err = client.Complete(t.Context(), openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{{Role: "user", Content: "hello"}},
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete error = %v, want %v", err, tt.wantErr)
			}
			if reporter.called {
				t.Fatalf("usage recorded after failed completion: %+v", reporter)
			}
			if !strings.Contains(logs.String(), "llm.request.failed") {
				t.Fatalf("logs missing failed event: %s", logs.String())
			}
			if strings.Contains(err.Error(), "private policy refusal") {
				t.Fatalf("error leaked refusal text: %v", err)
			}
		})
	}
}

func TestCompletePreservesInFlightContextErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		newCtx  func() (context.Context, context.CancelFunc)
		trigger func(context.CancelFunc, <-chan struct{})
		wantErr error
	}{
		{
			name: "canceled",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(t.Context())
			},
			trigger: func(cancel context.CancelFunc, started <-chan struct{}) {
				<-started
				cancel()
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(t.Context(), 100*time.Millisecond)
			},
			trigger: func(_ context.CancelFunc, started <-chan struct{}) {
				<-started
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &contextBlockingTransport{started: make(chan struct{})}
			client, err := openaipreset.New("https://openai.invalid", "test-key", "gpt-test",
				openaipreset.WithHTTPClient(&http.Client{Transport: transport}),
			)
			if err != nil {
				t.Fatalf("New error = %v", err)
			}

			ctx, cancel := tt.newCtx()
			defer cancel()
			triggered := make(chan struct{})
			go func() {
				tt.trigger(cancel, transport.started)
				close(triggered)
			}()

			_, err = client.Complete(ctx, openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{{Role: "user", Content: "hello"}},
			})
			<-triggered
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

func TestCompletionFromResponseTextSelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "prefers final answer",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\"tool_calls\":[]}","annotations":[]}]},{"id":"msg-1","type":"message","status":"completed","phase":"final_answer","role":"assistant","content":[{"type":"output_text","text":" final ","annotations":[]}]}]}`,
			want: "final",
		},
		{
			name: "skips tool envelope without final",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\"function_call\":{}}","annotations":[]}]}]}`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var resp responses.Response
			if err := json.Unmarshal([]byte(tc.raw), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			got := openaipreset.CompletionFromResponse(&resp, "fallback")
			if got.Text != tc.want {
				t.Fatalf("Text = %q, want %q", got.Text, tc.want)
			}
		})
	}
}

func assertMessagePayload(t *testing.T, raw any, role, content string) {
	t.Helper()
	message, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("message = %#v, want object", raw)
	}
	if message["role"] != role || message["content"] != content {
		t.Fatalf("message = %#v, want %s %s", message, role, content)
	}
}

func assertCompletionSchema(t *testing.T, raw any) {
	t.Helper()
	text, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("text = %#v, want object", raw)
	}
	format := text["format"].(map[string]any)
	if got := format["type"]; got != "json_schema" {
		t.Fatalf("format.type = %#v, want json_schema", got)
	}
	if got := strings.TrimSpace(format["name"].(string)); got != "answer_schema" {
		t.Fatalf("format.name = %#v, want answer_schema", got)
	}
	if got := format["strict"]; got != true {
		t.Fatalf("format.strict = %#v, want true", got)
	}
	assertJSONContains(t, format["schema"], `"type":"object"`)
}
