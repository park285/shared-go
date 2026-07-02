package openaipreset_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedllm "github.com/park285/shared-go/pkg/llm"
	"github.com/park285/shared-go/pkg/llm/openaipreset"
)

type recordingReporter struct {
	called   bool
	provider string
	model    string
	usage    sharedllm.Usage
}

func (r *recordingReporter) RecordUsage(_ context.Context, provider, model string, usage sharedllm.Usage) {
	r.called = true
	r.provider = provider
	r.model = model
	r.usage = usage
}

type flagTransport struct {
	used *bool
	base http.RoundTripper
}

func (t *flagTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	*t.used = true
	return t.base.RoundTrip(r)
}

const responsesBody = `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-returned","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"answer\":\"yes\"}","annotations":[]}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":17}}`

const chatBody = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-chat","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"answer\":\"no\"}"}}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestGenerateJSONResponses(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s, want /responses", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, responsesBody)
	}))
	defer server.Close()

	reporter := &recordingReporter{}
	client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
		openaipreset.WithSchemaName("event_summary"),
		openaipreset.WithTemperature(0.2),
		openaipreset.WithWebSearch(true),
		openaipreset.WithReasoningEffort("medium"),
		openaipreset.WithUsageReporter(reporter),
	)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	got, err := client.GenerateJSON(t.Context(), "system prompt", "user prompt", map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got != `{"answer":"yes"}` {
		t.Fatalf("text = %q, want responses JSON", got)
	}
	if !reporter.called || reporter.provider != "openai" || reporter.model != "gpt-returned" || reporter.usage.TotalTokens != 17 {
		t.Fatalf("reporter = %+v", reporter)
	}
	if got := payload["model"]; got != "gpt-test" {
		t.Fatalf("payload model = %#v, want gpt-test", got)
	}
	if got := payload["instructions"]; got != "system prompt" {
		t.Fatalf("payload instructions = %#v, want system prompt", got)
	}
	if got := payload["temperature"]; got != 0.2 {
		t.Fatalf("payload temperature = %#v, want 0.2", got)
	}
	assertJSONContains(t, payload["reasoning"], "medium")
	assertJSONContains(t, payload["tools"], "web_search")
	assertSchemaName(t, payload, "event_summary")
}

func TestGenerateJSONChatCompletions(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, chatBody)
	}))
	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
		openaipreset.WithChatCompletions(),
	)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	got, err := client.GenerateJSON(t.Context(), "system prompt", "user prompt", map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got != `{"answer":"no"}` {
		t.Fatalf("text = %q, want chat completions JSON", got)
	}
	assertJSONContains(t, payload["messages"], "system prompt")
	assertJSONContains(t, payload["messages"], "user prompt")
}

func TestGenerateJSONFallback(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/responses":
			http.Error(w, `{"error":{"message":"unsupported endpoint","type":"invalid_request_error","code":"unsupported_endpoint"}}`, http.StatusNotFound)
		case "/chat/completions":
			writeJSON(t, w, chatBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	got, err := client.GenerateJSON(t.Context(), "system prompt", "user prompt", map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got != `{"answer":"no"}` {
		t.Fatalf("text = %q, want fallback JSON", got)
	}
	if strings.Join(paths, ",") != "/responses,/chat/completions" {
		t.Fatalf("paths = %v, want responses then chat completions", paths)
	}
}

func TestGenerateJSONFallbackDisabled(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Error(w, `{"error":{"message":"unsupported endpoint","type":"invalid_request_error","code":"unsupported_endpoint"}}`, http.StatusNotFound)
	}))
	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
		openaipreset.WithAllowChatCompletionsFallback(false),
	)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	if _, err := client.GenerateJSON(t.Context(), "system prompt", "user prompt", map[string]any{"type": "object"}); err == nil {
		t.Fatal("GenerateJSON error = nil, want error when fallback disabled")
	}
	if strings.Join(paths, ",") != "/responses" {
		t.Fatalf("paths = %v, want no fallback", paths)
	}
}

func TestRunInto(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, responsesBody)
	}))
	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-5.5")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	var out struct {
		Answer string `json:"answer"`
	}
	if err := client.RunInto(t.Context(), "twentyq_verify_guess.judge", "user prompt", map[string]any{"type": "object"}, &out); err != nil {
		t.Fatalf("RunInto error = %v", err)
	}
	if out.Answer != "yes" {
		t.Fatalf("out.Answer = %q, want yes", out.Answer)
	}
	assertSchemaName(t, payload, "twentyq_verify_guess_judge")
}

func TestRunIntoNilOutput(t *testing.T) {
	client, err := openaipreset.New("https://example.invalid", "test-key", "gpt-5.5")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if err := client.RunInto(t.Context(), "task", "prompt", map[string]any{"type": "object"}, nil); err == nil {
		t.Fatal("RunInto nil output error = nil, want error")
	}
}

func TestNewRejectsEmptyModel(t *testing.T) {
	if _, err := openaipreset.New("https://example.invalid", "test-key", "   "); err == nil {
		t.Fatal("New empty model error = nil, want error")
	}
}

func TestNewRejectsEmptyAPIKey(t *testing.T) {
	if _, err := openaipreset.New("https://example.invalid", "  ", "gpt-test"); err == nil {
		t.Fatal("New empty api key error = nil, want error")
	}
}

func TestWithHTTPClientInjected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, responsesBody)
	}))
	defer server.Close()

	used := false
	injected := &http.Client{Transport: &flagTransport{used: &used, base: http.DefaultTransport}}
	client, err := openaipreset.New(server.URL, "test-key", "gpt-test",
		openaipreset.WithHTTPClient(injected),
	)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, err := client.GenerateJSON(t.Context(), "system prompt", "user prompt", map[string]any{"type": "object"}); err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if !used {
		t.Fatal("injected http client was not used")
	}
}

func assertJSONContains(t *testing.T, value any, want string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("value = %#v, want JSON containing %q", value, want)
	}
}

func assertSchemaName(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	text, ok := payload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text config = %#v, want object", payload["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format = %#v, want object", text["format"])
	}
	if got := format["name"]; got != want {
		t.Fatalf("text.format.name = %#v, want %s", got, want)
	}
}
