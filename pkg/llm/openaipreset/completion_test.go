package openaipreset_test

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

type contextBlockingTransport struct {
	started chan struct{}
	once    sync.Once
}

func TestMessageAlias(t *testing.T) {
	t.Parallel()

	shared := sharedllm.Message{Role: testUser, Content: "hello"}

	preset := shared

	if preset != shared {
		t.Fatalf("openaipreset.Message = %#v, want %#v", preset, shared)
	}
}

func (t *contextBlockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() { close(t.started) })
	<-req.Context().Done()

	return nil, req.Context().Err()
}

type completionPayloadCase struct {
	name  string
	req   openaipreset.CompletionRequest
	check func(t *testing.T, payload map[string]any)
}

func TestCompleteResponsesRequest(t *testing.T) {
	t.Parallel()

	cases := slices.Concat(
		completionPayloadOptionCases(),
		completionPayloadRoleCases(),
		completionPayloadProfileCases(),
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runCompletionPayloadCase(t, tc)
		})
	}
}

func completionPayloadOptionCases() []completionPayloadCase {
	temp := 1.0

	return []completionPayloadCase{
		{
			name: "basic",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testSystem, Content: " system prompt "},
					{Role: "assistant", Content: "previous"},
					{Role: testUser, Content: "   "},
					{Role: testUser, Content: " hello "},
				},
			},
			check: checkBasicPayload,
		},
		{
			name: "all options",
			req: openaipreset.CompletionRequest{
				Messages:        []openaipreset.Message{{Role: testDeveloper, Content: "stay terse"}},
				Model:           " gpt-override ",
				Temperature:     &temp,
				ReasoningEffort: " high ",
				WebSearch:       true,
				CacheKey:        " cache-1 ",
				ResponseFormat: &openaipreset.ResponseFormat{
					Name:   " answer_schema ",
					Schema: map[string]any{testFieldType: testObject},
					Strict: true,
				},
			},
			check: checkAllOptionsPayload,
		},
	}
}

func completionPayloadRoleCases() []completionPayloadCase {
	return []completionPayloadCase{
		{
			name: "role mapping",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testDeveloper, Content: "developer content"},
					{Role: testSystem, Content: "system content"},
					{Role: testUser, Content: "user content"},
					{Role: "assistant", Content: "assistant content"},
					{Role: "unknown", Content: "unknown content"},
					{Content: "empty role content"},
				},
			},
			check: checkRoleMappingPayload,
		},
		{
			name: "explicit cache breakpoint and mode",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testDeveloper, Content: "stable prefix", CacheBreakpoint: true},
					{Role: testUser, Content: "variable question"},
				},
				CacheKey:  "session-1",
				CacheMode: " explicit ",
			},
			check: checkExplicitCachePayload,
		},
	}
}

func completionPayloadProfileCases() []completionPayloadCase {
	openAIProfile := sharedllm.InstructionProfileOpenAI
	singleDeveloperProfile := sharedllm.InstructionProfileSingleDeveloper
	singleSystemProfile := sharedllm.InstructionProfileSingleSystem

	return []completionPayloadCase{
		{
			name: "openai instruction profile",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testSystem, Content: testInvariant},
					{Role: testDeveloper, Content: testDeveloper},
					{Role: testUser, Content: testQuestion},
				},
				InstructionProfile: &openAIProfile,
			},
			check: checkOpenAIProfilePayload,
		},
		{
			name: "single developer instruction profile",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testSystem, Content: testInvariant},
					{Role: testDeveloper, Content: testDeveloper},
					{Role: testUser, Content: testQuestion},
				},
				InstructionProfile: &singleDeveloperProfile,
			},
			check: checkSingleDeveloperProfilePayload,
		},
		{
			name: "single system instruction profile",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testSystem, Content: testInvariant},
					{Role: testDeveloper, Content: testDeveloper},
					{Role: testUser, Content: testQuestion},
				},
				InstructionProfile: &singleSystemProfile,
			},
			check: checkSingleSystemProfilePayload,
		},
	}
}

func runCompletionPayloadCase(t *testing.T, tc completionPayloadCase) {
	t.Helper()

	var payload map[string]any

	reporter := &recordingReporter{}

	var logs bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testResponses {
			t.Fatalf("path = %s, want /responses", r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want bearer test-key", got)
		}

		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
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

	assertCompletionSuccess(t, got, reporter, &logs)
	tc.check(t, payload)
}

func assertCompletionSuccess(t *testing.T, got openaipreset.CompletionResponse, reporter *recordingReporter, logs *bytes.Buffer) {
	t.Helper()

	if got.Text != `{"answer":"yes"}` {
		t.Fatalf("Text = %q, want response text", got.Text)
	}

	if got.Model != "gpt-returned" {
		t.Fatalf("Model = %q, want gpt-returned", got.Model)
	}

	if got.Usage != (sharedllm.Usage{InputTokens: 12, OutputTokens: 5, TotalTokens: 17, CachedInputTokens: 2, CacheWriteTokens: 7, ReasoningOutputTokens: 1}) {
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
}

func checkBasicPayload(t *testing.T, payload map[string]any) {
	t.Helper()

	if got := payload["model"]; got != "gpt-test" {
		t.Fatalf("model = %#v, want gpt-test", got)
	}

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	if len(input) != 3 {
		t.Fatalf("input len = %d, want 3", len(input))
	}

	assertMessagePayload(t, input[0], testSystem, "system prompt")
	assertMessagePayload(t, input[1], "assistant", "previous")
	assertMessagePayload(t, input[2], testUser, "hello")

	if _, ok := payload["temperature"]; ok {
		t.Fatalf("temperature = %#v, want omitted", payload["temperature"])
	}
}

func checkAllOptionsPayload(t *testing.T, payload map[string]any) {
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

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	assertMessagePayload(t, input[0], testDeveloper, "stay terse")
}

func checkRoleMappingPayload(t *testing.T, payload map[string]any) {
	t.Helper()

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	want := []struct {
		role    string
		content string
	}{
		{role: testDeveloper, content: "developer content"},
		{role: testSystem, content: "system content"},
		{role: testUser, content: "user content"},
		{role: "assistant", content: "assistant content"},
		{role: testUser, content: "unknown content"},
		{role: testUser, content: "empty role content"},
	}

	if len(input) != len(want) {
		t.Fatalf("input len = %d, want %d", len(input), len(want))
	}

	for i := range want {
		assertMessagePayload(t, input[i], want[i].role, want[i].content)
	}
}

func checkExplicitCachePayload(t *testing.T, payload map[string]any) {
	t.Helper()

	if got := payload["prompt_cache_key"]; got != "session-1" {
		t.Fatalf("prompt_cache_key = %#v, want session-1", got)
	}

	options, ok := payload["prompt_cache_options"].(map[string]any)
	if !ok || options["mode"] != "explicit" {
		t.Fatalf("prompt_cache_options = %#v, want mode explicit", payload["prompt_cache_options"])
	}

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2", len(input))
	}

	assertCacheBreakpointBlock(t, input[0])
	assertMessagePayload(t, input[1], testUser, "variable question")
}

func assertCacheBreakpointBlock(t *testing.T, raw any) {
	t.Helper()

	first := testsupport.AssertType[map[string]any](t, "input[0]", raw)
	if got := first["role"]; got != testDeveloper {
		t.Fatalf("breakpoint message role = %#v, want developer", got)
	}

	blocks, ok := first["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("breakpoint content = %#v, want single block list", first["content"])
	}

	block := testsupport.AssertType[map[string]any](t, "blocks[0]", blocks[0])
	if got := block[testFieldType]; got != "input_text" {
		t.Fatalf("block type = %#v, want input_text", got)
	}

	if got := block["text"]; got != "stable prefix" {
		t.Fatalf("block text = %#v, want stable prefix", got)
	}

	breakpoint, ok := block["prompt_cache_breakpoint"].(map[string]any)
	if !ok || breakpoint["mode"] != "explicit" {
		t.Fatalf("prompt_cache_breakpoint = %#v, want mode explicit", block["prompt_cache_breakpoint"])
	}
}

func checkOpenAIProfilePayload(t *testing.T, payload map[string]any) {
	t.Helper()

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	if len(input) != 3 {
		t.Fatalf("input len = %d, want 3", len(input))
	}

	assertMessagePayload(t, input[0], testDeveloper, "[APPLICATION INVARIANTS]\ninvariant")
	assertMessagePayload(t, input[1], testDeveloper, "[DEVELOPER INSTRUCTIONS]\ndeveloper")
	assertMessagePayload(t, input[2], testUser, testQuestion)
}

func checkSingleDeveloperProfilePayload(t *testing.T, payload map[string]any) {
	t.Helper()

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2", len(input))
	}

	assertMessagePayload(t, input[0], testDeveloper, "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper")
	assertMessagePayload(t, input[1], testUser, testQuestion)
}

func checkSingleSystemProfilePayload(t *testing.T, payload map[string]any) {
	t.Helper()

	input := testsupport.AssertType[[]any](t, "payload['input']", payload["input"])
	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2", len(input))
	}

	assertMessagePayload(t, input[0], testSystem, "[APPLICATION INVARIANTS]\ninvariant\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper")
	assertMessagePayload(t, input[1], testUser, testQuestion)
}

// 서브테스트가 모두 끝난 뒤 requestCount를 검사하므로 부모를 병렬화하지 않는다.
func TestCompleteRejectsInvalidInstructionAdaptationBeforeNetwork(t *testing.T) {
	var requestCount atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		writeJSON(t, w, responsesBody)
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	openAIProfile := sharedllm.InstructionProfileOpenAI
	invalidProfile := sharedllm.InstructionProfile(255)
	tests := []struct {
		name    string
		req     openaipreset.CompletionRequest
		wantErr error
	}{
		{
			name: "invalid sequence",
			req: openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{
					{Role: testUser, Content: testQuestion},
					{Role: testDeveloper, Content: "late instruction"},
				},
				InstructionProfile: &openAIProfile,
			},
			wantErr: sharedllm.ErrInvalidInstructionSequence,
		},
		{
			name: "invalid profile",
			req: openaipreset.CompletionRequest{
				Messages:           []openaipreset.Message{{Role: testUser, Content: testQuestion}},
				InstructionProfile: &invalidProfile,
			},
			wantErr: sharedllm.ErrInvalidInstructionProfile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Complete(t.Context(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}

	if got := requestCount.Load(); got != 0 {
		t.Fatalf("network request count = %d, want 0", got)
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
				Messages: []openaipreset.Message{{Role: testUser, Content: "hello"}},
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

func TestCompleteNilClientReturnsStableError(t *testing.T) {
	t.Parallel()

	var client *openaipreset.Client

	_, err := client.Complete(t.Context(), openaipreset.CompletionRequest{})

	if err == nil || err.Error() != "openaipreset: client is nil" {
		t.Fatalf("Complete error = %v, want stable nil-client error", err)
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
				Messages: []openaipreset.Message{{Role: testUser, Content: "hello"}},
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
		{
			name: "skips formatted tool envelope with reordered key",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\n  \"metadata\": {},\n  \"tool_calls\": []\n}","annotations":[]}]}]}`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var resp responses.Response

			if err := jsonv2.Unmarshal([]byte(tc.raw), &resp); err != nil {
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

	format := testsupport.AssertType[map[string]any](t, "text['format']", text["format"])
	if got := format[testFieldType]; got != "json_schema" {
		t.Fatalf("format.type = %#v, want json_schema", got)
	}

	if got := strings.TrimSpace(testsupport.AssertType[string](t, "format['name']", format["name"])); got != "answer_schema" {
		t.Fatalf("format.name = %#v, want answer_schema", got)
	}

	if got := format["strict"]; got != true {
		t.Fatalf("format.strict = %#v, want true", got)
	}

	assertJSONContains(t, format["schema"], `"type":"object"`)
}
