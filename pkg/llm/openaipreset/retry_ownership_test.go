package openaipreset_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

func TestNewRequiresExplicitMaxRetries(t *testing.T) {
	t.Parallel()

	for _, opts := range [][]openaipreset.Option{nil, {openaipreset.WithMaxRetries(-1)}} {
		if _, err := openaipreset.New("https://example.invalid", "test-key", "gpt-test", opts...); err == nil {
			t.Fatalf("New(%d options) error = nil, want explicit max retries error", len(opts))
		}
	}
}

func TestClientRetryAttemptsAreOwnedByOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		opts         []openaipreset.Option
		wantRequests int64
	}{
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
				Messages: []sharedllm.Message{{Role: testUser, Content: "hi"}},
			}); err == nil {
				t.Fatal("Complete() error = nil, want provider failure")
			}

			if got := requests.Load(); got != tt.wantRequests {
				t.Fatalf("provider request count = %d, want %d", got, tt.wantRequests)
			}
		})
	}
}
