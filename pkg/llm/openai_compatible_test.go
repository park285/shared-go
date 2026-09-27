package llm

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestOpenAICompatibleJSONGeneratorResponsesStructuredRequest(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertResponsesRequestEnvelope(t, r)

		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-returned","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":17}}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	temperature := 0.2
	reporter := &recordingUsageReporter{}

	got, err := RunJSON(t.Context(), generator, JSONRequest{
		TaskName:        "summarize",
		InvariantPrompt: "invariant prompt",
		DeveloperPrompt: "developer prompt",
		UserPrompt:      "user prompt",
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           testGptTest,
		Temperature:     &temperature,
		ReasoningEffort: "medium",
		WebSearch:       true,
	}, "openai", reporter)
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}

	assertStructuredResponsesResult(t, got, reporter)
	assertStructuredRequestPayload(t, payload)
}

func assertResponsesRequestEnvelope(t *testing.T, r *http.Request) {
	t.Helper()

	if r.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", r.Method)
	}

	if r.URL.Path != testResponses {
		t.Errorf("path = %s, want /responses", r.URL.Path)
	}

	if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("authorization = %q, want bearer test-key", got)
	}
}

func assertStructuredResponsesResult(t *testing.T, got JSONResponse, reporter *recordingUsageReporter) {
	t.Helper()

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
}

func assertStructuredRequestPayload(t *testing.T, payload map[string]any) {
	t.Helper()

	if got := payload["model"]; got != testGptTest {
		t.Fatalf("payload model = %#v, want gpt-test", got)
	}

	if got, exists := payload["instructions"]; exists {
		t.Fatalf("payload instructions = %#v, want omitted (instructions travel as developer layers)", got)
	}

	assertResponsesInputRoles(t, payload["input"], []string{roleDeveloper, roleDeveloper, roleUser})
	assertJSONContains(t, payload["input"], applicationInvariantsLabel)
	assertJSONContains(t, payload["input"], "invariant prompt")
	assertJSONContains(t, payload["input"], developerInstructionsLabel)
	assertJSONContains(t, payload["input"], "developer prompt")
	assertJSONContains(t, payload["input"], "user prompt")

	if got := payload["temperature"]; got != 0.2 {
		t.Fatalf("payload temperature = %#v, want 0.2", got)
	}

	assertJSONContains(t, payload["reasoning"], "medium")
	assertJSONContains(t, payload["tools"], "web_search")
	assertStructuredResponsesFormat(t, payload["text"], "summary")
}

func TestOpenAICompatibleJSONGeneratorChatCompletionsStructuredOutput(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}

		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeJSON(t, w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-chat","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Here is JSON: {\"ok\":true}"}}],"usage":{"prompt_tokens":7,"prompt_tokens_details":{"cached_tokens":1},"completion_tokens":3,"completion_tokens_details":{"reasoning_tokens":2},"total_tokens":10}}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	got, err := generator.GenerateJSON(t.Context(), JSONRequest{
		DeveloperPrompt: "developer prompt",
		UserPrompt:      "user prompt",
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           testGptTest,
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

	if got := payload["model"]; got != testGptTest {
		t.Fatalf("payload model = %#v, want gpt-test", got)
	}

	assertJSONContains(t, payload["messages"], developerInstructionsLabel)
	assertJSONContains(t, payload["messages"], "developer prompt")
	assertJSONContains(t, payload["messages"], "user prompt")
	assertJSONContains(t, payload["messages"], "type")
	assertJSONContains(t, payload["messages"], "object")
	assertJSONContains(t, payload["reasoning_effort"], "low")
}

// 지시 계층이 모두 비어도 단일 계층 경로를 탄다. Responses 요청은 instructions 없이 user 메시지 하나만
// 보내고, 계층 label을 만들지 않는다.
func TestOpenAICompatibleJSONGeneratorResponsesWithoutInstructionLayers(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: testTestKey})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	req := validJSONRequest()

	req.InvariantPrompt = " \t"
	req.DeveloperPrompt = "\n"

	if _, err := generator.GenerateJSON(t.Context(), req); err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}

	if got, exists := payload["instructions"]; exists {
		t.Fatalf("payload instructions = %#v, want omitted", got)
	}

	assertResponsesInputRoles(t, payload["input"], []string{roleUser})

	if got := chatMessages(t, payload["input"])[0]["content"]; got != req.UserPrompt {
		t.Fatalf("input[0].content = %#v, want %q", got, req.UserPrompt)
	}

	if containsJSON(t, payload["input"], applicationInvariantsLabel) || containsJSON(t, payload["input"], developerInstructionsLabel) {
		t.Fatalf("payload input = %#v, want layer labels omitted", payload["input"])
	}
}

// 지시 계층이 모두 비면 Chat Completions의 system 메시지는 schema 지시만 담고, user 입력을 지시로
// 끌어올리지 않는다.
func TestOpenAICompatibleJSONGeneratorChatCompletionsWithoutInstructionLayers(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeJSON(t, w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-chat","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"ok\":true}"}}]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: testTestKey})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	req := validJSONRequest()

	req.DeveloperPrompt = ""
	req.ChatCompletions = true

	_, err = generator.GenerateJSON(t.Context(), req)
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}

	wantSystem, err := chatCompletionsSystemPrompt("", req.Schema)
	if err != nil {
		t.Fatalf("chatCompletionsSystemPrompt error = %v", err)
	}

	messages := chatMessages(t, payload["messages"])
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want system and user", messages)
	}

	if messages[0]["role"] != roleSystem || messages[0]["content"] != wantSystem {
		t.Fatalf("messages[0] = %#v, want schema-only system message", messages[0])
	}

	if messages[1]["role"] != roleUser || messages[1]["content"] != req.UserPrompt {
		t.Fatalf("messages[1] = %#v, want user prompt", messages[1])
	}
}

func TestOpenAICompatibleJSONGeneratorUnsupportedEndpointUsesResponsesOnly(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		http.Error(w, `{"error":{"message":"private provider detail","type":"invalid_request_error","code":"unsupported_endpoint"}}`, http.StatusNotFound)
	}))

	defer server.Close()

	zeroRetries := 0

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL, APIKey: testTestKey, MaxRetries: &zeroRetries,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator: %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validJSONRequest())
	if err == nil || strings.Contains(err.Error(), "private provider detail") {
		t.Fatalf("GenerateJSON error = %v, want sanitized provider error", err)
	}

	if strings.Join(paths, ",") != testResponses {
		t.Fatalf("paths = %v, want one Responses request", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorRefusalUsesResponsesOnly(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"private policy refusal"}]}]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
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

	if strings.Join(paths, ",") != testResponses {
		t.Fatalf("paths = %v, want no fallback", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorEmptyOutputUsesResponsesOnly(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validJSONRequest())
	if !errors.Is(err, ErrOpenAIEmptyOutput) {
		t.Fatalf("GenerateJSON error = %v, want ErrOpenAIEmptyOutput", err)
	}

	if strings.Join(paths, ",") != testResponses {
		t.Fatalf("paths = %v, want no fallback", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorRejectsNullChatCompletion(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(t, w, `null`)
	}))

	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	req := validJSONRequest()

	req.ChatCompletions = true

	_, err = generator.GenerateJSON(t.Context(), req)
	if !errors.Is(err, ErrOpenAIEmptyOutput) {
		t.Fatalf("GenerateJSON error = %v, want ErrOpenAIEmptyOutput", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestOpenAICompatibleJSONGeneratorServerErrorUsesResponsesOnly(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		http.Error(w, `{"error":{"message":"unavailable","type":"server_error","code":"server_error"}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	zeroRetries := 0

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL:    server.URL,
		APIKey:     testTestKey,
		MaxRetries: &zeroRetries,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validJSONRequest())
	if err == nil {
		t.Fatal("GenerateJSON error = nil, want server error")
	}

	if strings.Join(paths, ",") != testResponses {
		t.Fatalf("paths = %v, want /responses without chat completions", paths)
	}
}

func TestOpenAICompatibleJSONGeneratorEmptyOutputDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[]}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  testTestKey,
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

	testsupport.WriteResponse(t, w, body)
}

func containsJSON(t *testing.T, value any, want string) bool {
	t.Helper()

	raw, err := jsonv2.Marshal(value)
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

func chatMessages(t *testing.T, raw any) []map[string]any {
	t.Helper()

	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("messages = %#v, want array", raw)
	}

	out := make([]map[string]any, 0, len(list))

	for _, item := range list {
		message, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("message = %#v, want object", item)
		}

		out = append(out, message)
	}

	return out
}

func assertResponsesInputRoles(t *testing.T, raw any, want []string) {
	t.Helper()

	messages := chatMessages(t, raw)
	roles := make([]string, 0, len(messages))

	for _, message := range messages {
		roles = append(roles, fmt.Sprint(message["role"]))
	}

	if !slices.Equal(roles, want) {
		t.Fatalf("input roles = %q, want %q", roles, want)
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
		if got := ResponsesSchemaName(c.in); got != c.want {
			t.Fatalf("%s: ResponsesSchemaName(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}

	if got := ResponsesSchemaName(strings.Repeat("a", 80)); len(got) != 64 {
		t.Fatalf("64자 cap 실패: len=%d", len(got))
	}
}

func TestOpenAICompatibleJSONGeneratorForwardsPromptCacheKey(t *testing.T) {
	var payload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoded := map[string]any{}
		if err := jsonv2.UnmarshalRead(r.Body, &decoded); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		payload = decoded

		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	generator, err := NewOpenAICompatibleJSONGenerator(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: testTestKey})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleJSONGenerator error = %v", err)
	}

	base := JSONRequest{
		TaskName:        "summarize",
		DeveloperPrompt: "developer prompt",
		UserPrompt:      "user prompt",
		SchemaName:      "summary",
		Schema:          map[string]any{"type": "object"},
		Model:           testGptTest,
	}

	withKey := base

	withKey.CacheKey = " tq:answer "

	if _, err := RunJSON(t.Context(), generator, withKey, "openai", nil); err != nil {
		t.Fatalf("RunJSON with cache key error = %v", err)
	}

	if got := payload["prompt_cache_key"]; got != "tq:answer" {
		t.Fatalf("payload prompt_cache_key = %#v, want tq:answer (trimmed)", got)
	}

	if _, err := RunJSON(t.Context(), generator, base, "openai", nil); err != nil {
		t.Fatalf("RunJSON without cache key error = %v", err)
	}

	if _, exists := payload["prompt_cache_key"]; exists {
		t.Fatalf("payload carries prompt_cache_key without CacheKey: %#v", payload["prompt_cache_key"])
	}
}
