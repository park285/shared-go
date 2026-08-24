package httputil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestJSONClient_DoForwardsRequestAndReturnsResponse(t *testing.T) {
	t.Parallel()

	ts := newEchoServer(t)

	client := &JSONClient{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/ping", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Do() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if got := string(body); got != "pong" {
		t.Fatalf("Do() body = %q, want %q", got, "pong")
	}
}

func TestJSONClient_DoWrapsError(t *testing.T) {
	t.Parallel()

	client := &JSONClient{
		baseURL:    "http://127.0.0.1:0",
		httpClient: &http.Client{Timeout: time.Millisecond},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:0/unreachable", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("Body.Close() error = %v", closeErr)
		}

		t.Fatal("Do() expected error for unreachable host")
	}

	if !strings.HasPrefix(err.Error(), "request: ") {
		t.Fatalf("Do() error = %q, want prefix %q", err.Error(), "request: ")
	}
}

func TestJSONClient_CheckStatusDelegatesToStandalone(t *testing.T) {
	t.Parallel()

	client := &JSONClient{}

	t.Run("200 OK는 nil", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}
		if err := client.CheckStatus(resp); err != nil {
			t.Fatalf("CheckStatus() error = %v", err)
		}
	})

	t.Run("500은 에러", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("fail")),
		}

		err := client.CheckStatus(resp)
		if err == nil {
			t.Fatal("CheckStatus() expected error")
		}

		if !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("CheckStatus() error = %q, want status 500", err.Error())
		}
	})
}

func TestJSONClient_DecodeJSONDelegatesToStandalone(t *testing.T) {
	t.Parallel()

	client := &JSONClient{}
	rc := &trackCloseReadCloser{Reader: strings.NewReader(`{"value":42}`)}
	resp := &http.Response{Body: rc}

	type payload struct {
		Value int `json:"value"`
	}

	out, err := client.DecodeJSON[payload](resp)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}

	if out.Value != 42 {
		t.Fatalf("DecodeJSON() value = %d, want 42", out.Value)
	}

	if !rc.closed {
		t.Fatal("DecodeJSON() expected body close")
	}
}

func TestJSONClient_DiscardBodyNilCases(t *testing.T) {
	t.Parallel()

	client := &JSONClient{}

	t.Run("nil resp", func(t *testing.T) {
		t.Parallel()

		if err := client.DiscardBody(nil); err != nil {
			t.Fatalf("DiscardBody(nil) error = %v", err)
		}
	})

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()

		if err := client.DiscardBody(&http.Response{Body: nil}); err != nil {
			t.Fatalf("DiscardBody(nil body) error = %v", err)
		}
	})
}

func TestJSONClient_ApplyAPIKeyNilReq(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyAPIKey panicked with nil req: %v", r)
		}
	}()

	client := NewJSONClient("https://example.com", "key", time.Second)
	client.applyAPIKey(nil)
}

func TestJSONClient_DiscardBodyReadError(t *testing.T) {
	t.Parallel()

	client := &JSONClient{}
	resp := &http.Response{
		Body: &errorReadCloser{err: errors.New("disk full")},
	}

	err := client.DiscardBody(resp)
	if err == nil {
		t.Fatal("DiscardBody() expected error for failing reader")
	}

	if !strings.Contains(err.Error(), "discard body") {
		t.Fatalf("DiscardBody() error = %q, want 'discard body' prefix", err.Error())
	}
}

func newEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "pong")
	}))
	t.Cleanup(ts.Close)

	return ts
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := f(req)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return out, nil
}
