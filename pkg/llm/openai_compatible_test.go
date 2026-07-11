package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAICompatibleJSONGeneratorResponsesStructuredRequest(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-returned","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":17}}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}
	temperature := 0.2
	reporter := &recordingUsageReporter{}

	got, err := RunJSON(t.Context(), generator, JSONRequest{
		TaskName:        "summarize",
		SystemPrompt:    "system prompt",
		UserPrompt:      "user prompt",
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           "gpt-test",
		Temperature:     &temperature,
		ReasoningEffort: "medium",
		WebSearch:       true,
	}, "openai", reporter)
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}

	if got.Text != `{"ok":true}` {
		t.Fatalf("Text = %q, want JSON output", got.Text)
	}
	if got.Model != "gpt-returned" {
		t.Fatalf("Model = %q, want gpt-returned", got.Model)
	}
	if got.Usage != (Usage{InputTokens: 12, OutputTokens: 5, TotalTokens: 17, CachedInputTokens: 2, ReasoningOutputTokens: 1}) {
		t.Fatalf("Usage = %+v", got.Usage)
	}
	if !reporter.called || reporter.model != "gpt-returned" || reporter.usage.TotalTokens != 17 {
		t.Fatalf("usage reporter = called:%v model:%q usage:%+v", reporter.called, reporter.model, reporter.usage)
	}

	if got := payload["model"]; got != "gpt-test" {
		t.Fatalf("payload model = %#v, want gpt-test", got)
	}
	if got := payload["instructions"]; got != "system prompt" {
		t.Fatalf("payload instructions = %#v, want system prompt", got)
	}
	if !containsJSON(t, payload["input"], "user prompt") {
		t.Fatalf("payload input = %#v, want user prompt", payload["input"])
	}
	if got := payload["temperature"]; got != 0.2 {
		t.Fatalf("payload temperature = %#v, want 0.2", got)
	}
	assertJSONContains(t, payload["reasoning"], "medium")
	assertJSONContains(t, payload["tools"], "web_search")
	assertStructuredResponsesFormat(t, payload["text"], "summary")
}

func TestOpenAICompatibleJSONGeneratorRejectsMixedPromptStylesBeforeNetwork(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		writeJSON(t, w, `{}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	tests := []struct {
		name            string
		chatCompletions bool
		setLayer        func(*JSONRequest)
	}{
		{
			name: "responses invariant",
			setLayer: func(req *JSONRequest) {
				req.InvariantPrompt = "invariant"
			},
		},
		{
			name: "responses developer",
			setLayer: func(req *JSONRequest) {
				req.DeveloperPrompt = "developer"
			},
		},
		{
			name:            "chat completions invariant",
			chatCompletions: true,
			setLayer: func(req *JSONRequest) {
				req.InvariantPrompt = "invariant"
			},
		},
		{
			name:            "chat completions developer",
			chatCompletions: true,
			setLayer: func(req *JSONRequest) {
				req.DeveloperPrompt = "developer"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validJSONRequest()
			req.ChatCompletions = tt.chatCompletions
			tt.setLayer(&req)

			_, err := generator.GenerateJSON(t.Context(), req)
			if !errors.Is(err, ErrInvalidJSONRequest) {
				t.Fatalf("GenerateJSON error = %v, want ErrInvalidJSONRequest", err)
			}
			if !strings.Contains(err.Error(), "system prompt") {
				t.Fatalf("GenerateJSON error = %q, want mixed prompt validation detail", err)
			}
		})
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("network request count = %d, want 0", got)
	}
}

func TestOpenAICompatibleJSONGeneratorChatCompletionsStructuredOutput(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-chat","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Here is JSON: {\"ok\":true}"}}],"usage":{"prompt_tokens":7,"prompt_tokens_details":{"cached_tokens":1},"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":2},"total_tokens":10}}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	got, err := generator.GenerateJSON(t.Context(), JSONRequest{
		SystemPrompt:    "system prompt",
		UserPrompt:      "user prompt",
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           "gpt-test",
		ReasoningEffort: "low",
		ChatCompletions: true,
	})
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got.Text != `{"ok":true}` {
		t.Fatalf("Text = %q, want extracted JSON", got.Text)
	}
	if got.Usage != (Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10, CachedInputTokens: 1, ReasoningOutputTokens: 2}) {
		t.Fatalf("Usage = %+v", got.Usage)
	}

	if got := payload["model"]; got != "gpt-test" {
		t.Fatalf("payload model = %#v, want gpt-test", got)
	}
	assertJSONContains(t, payload["messages"], "system prompt")
	assertJSONContains(t, payload["messages"], "user prompt")
	assertJSONContains(t, payload["messages"], "type")
	assertJSONContains(t, payload["messages"], "object")
	assertJSONContains(t, payload["reasoning_effort"], "low")
}

func TestOpenAICompatibleJSONGeneratorFallsBackToChatCompletions(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/responses":
			http.Error(w, `{"error":{"message":"unsupported endpoint","type":"invalid_request_error","code":"unsupported_endpoint"}}`, http.StatusNotFound)
		case "/chat/completions":
			writeJSON(t, w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-chat","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"fallback\":true}"}}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL:                      server.URL,
		APIKey:                       "test-key",
		AllowChatCompletionsFallback: true,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	got, err := generator.GenerateJSON(t.Context(), validJSONRequest())
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got.Text != `{"fallback":true}` {
		t.Fatalf("Text = %q, want fallback JSON", got.Text)
	}
	if !got.FallbackUsed {
		t.Fatal("FallbackUsed = false, want true")
	}
	if strings.Join(paths, ",") != "/responses,/chat/completions" {
		t.Fatalf("paths = %v, want responses then chat completions", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorDoesNotFallbackOnRefusal(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"private policy refusal"}]}]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL:                      server.URL,
		APIKey:                       "test-key",
		AllowChatCompletionsFallback: true,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validJSONRequest())
	if err == nil {
		t.Fatal("GenerateJSON refusal error = nil, want error")
	}
	if !strings.Contains(err.Error(), "refusal=true") {
		t.Fatalf("error = %q, want refusal diagnostic", err)
	}
	if strings.Contains(err.Error(), "private policy refusal") {
		t.Fatalf("error leaked refusal text: %q", err)
	}
	if strings.Join(paths, ",") != "/responses" {
		t.Fatalf("paths = %v, want no fallback", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorEmptyOutputDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validJSONRequest())
	if !errors.Is(err, ErrOpenAIEmptyOutput) {
		t.Fatalf("GenerateJSON error = %v, want ErrOpenAIEmptyOutput", err)
	}
	if !strings.Contains(err.Error(), "status=completed") {
		t.Fatalf("error = %q, want response status diagnostic", err)
	}
	if strings.Contains(err.Error(), "output=[]") {
		t.Fatalf("error exposed raw output payload: %q", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func containsJSON(t *testing.T, value any, want string) bool {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return strings.Contains(string(raw), want)
}

func assertJSONContains(t *testing.T, value any, want string) {
	t.Helper()
	if !containsJSON(t, value, want) {
		t.Fatalf("value = %#v, want JSON containing %q", value, want)
	}
}

func assertStructuredResponsesFormat(t *testing.T, raw any, name string) {
	t.Helper()

	text, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("text config = %#v, want object", raw)
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format = %#v, want object", text["format"])
	}
	if got := format["type"]; got != "json_schema" {
		t.Fatalf("text.format.type = %#v, want json_schema", got)
	}
	if got := format["name"]; got != name {
		t.Fatalf("text.format.name = %#v, want %s", got, name)
	}
	if got := format["strict"]; got != true {
		t.Fatalf("text.format.strict = %#v, want true", got)
	}
	assertJSONContains(t, format["schema"], `"type":"object"`)
}

func TestSanitizeResponsesSchemaName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"점 포함 judge name", "twentyq_verify_guess.strict_identity_judge_01", "twentyq_verify_guess_strict_identity_judge_01"},
		{"이미 유효", "twentyq_answer_question", "twentyq_answer_question"},
		{"공백만", "   ", "schema"},
		{"빈 문자열", "", "schema"},
		{"여러 비허용 문자", "a.b/c d", "a_b_c_d"},
	}
	for _, c := range cases {
		if got := sanitizeResponsesSchemaName(c.in); got != c.want {
			t.Fatalf("%s: sanitizeResponsesSchemaName(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	if got := sanitizeResponsesSchemaName(strings.Repeat("a", 80)); len(got) != 64 {
		t.Fatalf("64자 cap 실패: len=%d", len(got))
	}
}
