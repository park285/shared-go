package openaipreset_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

func TestResolveOpenAIMaxRetries(t *testing.T) {
	t.Parallel()

	zero, negative, explicit := 0, -3, 5
	tests := []struct {
		name       string
		configured *int
		want       int
	}{
		{name: "unset pins sdk default", configured: nil, want: 2},
		{name: "zero disables retries", configured: &zero, want: 0},
		{name: "negative clamps instead of panicking", configured: &negative, want: 0},
		{name: "explicit value honored", configured: &explicit, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sharedllm.ResolveOpenAIMaxRetries(tt.configured); got != tt.want {
				t.Fatalf("ResolveOpenAIMaxRetries() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClientRetryAttemptsAreOwnedByOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		opts         []openaipreset.Option
		wantRequests int64
	}{
		{name: "default preserves sdk behavior", wantRequests: 1 + sharedllm.DefaultOpenAIMaxRetries},
		{name: "zero leaves retry to the consumer", opts: []openaipreset.Option{openaipreset.WithMaxRetries(0)}, wantRequests: 1},
		{name: "explicit budget is honored", opts: []openaipreset.Option{openaipreset.WithMaxRetries(1)}, wantRequests: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int64

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))

			defer server.Close()

			client, err := openaipreset.New(server.URL, "test-key", "gpt-test", tt.opts...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if _, err := client.Complete(t.Context(), openaipreset.CompletionRequest{
				Messages: []openaipreset.Message{{Role: testUser, Content: "hi"}},
			}); err == nil {
				t.Fatal("Complete() error = nil, want provider failure")
			}

			if got := requests.Load(); got != tt.wantRequests {
				t.Fatalf("provider request count = %d, want %d", got, tt.wantRequests)
			}
		})
	}
}

func TestGenerateJSONAsDecodeErrorPreservesCauseClass(t *testing.T) {
	t.Parallel()

	const secret = "Bearer provider-canary-must-not-appear"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"`+secret+`","annotations":[]}]}]}`)
	}))

	defer server.Close()

	client, err := openaipreset.New(server.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.GenerateJSONAs[answerPayload](t.Context(), "decode-test", testPromptLayers(), map[string]any{testFieldType: testObject})
	if err == nil {
		t.Fatal("GenerateJSONAs() error = nil, want decode failure")
	}

	if strings.Contains(err.Error(), "provider-canary") || strings.Contains(err.Error(), secret) {
		t.Fatalf("decode error leaked provider output: %v", err)
	}

	if !strings.Contains(err.Error(), "decode decode-test json failed") {
		t.Fatalf("decode error lost task context: %v", err)
	}

	unwrapped := err

	for {
		next := errors.Unwrap(unwrapped)
		if next == nil {
			break
		}

		unwrapped = next
	}

	if errors.Is(unwrapped, err) {
		t.Fatal("decode error erased its cause; want the decoder error reachable via Unwrap")
	}
}
