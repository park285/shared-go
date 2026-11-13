package openaipreset_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
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

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return resp, nil
}

const responsesBody = `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-returned","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"answer\":\"yes\"}","annotations":[]}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":7},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":17}}`

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")

	testsupport.WriteResponse(t, w, body)
}

func TestGenerateLayeredResponsesJSONReturnsCompleteResponsesSurface(t *testing.T) {
	var (
		calls   atomic.Int32
		payload map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if err := jsonv2.UnmarshalRead(r.Body, &payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		writeResponsesCompleted(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"before ","annotations":[]},{"type":"output_text","text":"{\"answer\":\"yes\"} after","annotations":[]}]}]}`)
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test", openaipreset.WithMaxRetries(0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := client.GenerateLayeredResponsesJSON(t.Context(), "twentyq_verify_guess.strict_identity_judge_01", openaipreset.PromptLayers{Invariant: testInvariant, Developer: testDeveloper, User: testUser}, map[string]any{testFieldType: testObject})
	if err != nil {
		t.Fatalf("GenerateLayeredResponsesJSON: %v", err)
	}

	if got != `before {"answer":"yes"} after` {
		t.Fatalf("raw output = %q", got)
	}

	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}

	assertSchemaName(t, payload, "twentyq_verify_guess_strict_identity_judge_01")

	if payload["stream"] != true {
		t.Fatalf("stream = %v, want true", payload["stream"])
	}
}

func TestGenerateLayeredResponsesJSONPreservesEmptyOutputSentinel(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeResponsesCompleted(t, w, `{"id":"resp-empty","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[]}`)
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test", openaipreset.WithMaxRetries(0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := client.GenerateLayeredResponsesJSON(t.Context(), "task", openaipreset.PromptLayers{
		Invariant: testInvariant,
		Developer: testDeveloper,
		User:      testUser,
	}, map[string]any{testFieldType: testObject})
	if !errors.Is(err, sharedllm.ErrOpenAIEmptyOutput) {
		t.Fatalf("GenerateLayeredResponsesJSON error = %v, want ErrOpenAIEmptyOutput", err)
	}

	if got != "" {
		t.Fatalf("GenerateLayeredResponsesJSON output = %q, want empty", got)
	}

	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestGenerateLayeredResponsesJSONSanitizesProviderError(t *testing.T) {
	t.Parallel()

	const marker = "PRIVATE_PROVIDER_ERROR_MARKER"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"`+marker+`","type":"invalid_request_error","code":"bad_request"}}`, http.StatusBadRequest)
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test", openaipreset.WithMaxRetries(0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.GenerateLayeredResponsesJSON(t.Context(), "task", testPromptLayers(), map[string]any{testFieldType: testObject})
	if err == nil {
		t.Fatal("GenerateLayeredResponsesJSON error = nil")
	}

	if strings.Contains(err.Error(), marker) {
		t.Fatalf("provider error marker leaked: %v", err)
	}
}
