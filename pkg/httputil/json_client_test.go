package httputil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSONClient_NewJSONRequestSetsHeadersAndBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com/", " secret-key ", 5*time.Second)

	req, err := client.NewJSONRequest(context.Background(), http.MethodPost, "/internal/test", map[string]any{
		"name": "kapu",
		"id":   7,
	})
	if err != nil {
		t.Fatalf("NewJSONRequest() error = %v", err)
	}

	if got, want := req.Method, http.MethodPost; got != want {
		t.Fatalf("req.Method = %s, want %s", got, want)
	}
	if got, want := req.URL.String(), "https://example.com/internal/test"; got != want {
		t.Fatalf("req.URL = %s, want %s", got, want)
	}
	if got, want := req.Header.Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := req.Header.Get(apiKeyHeader), "secret-key"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	bodyText := string(body)
	if !strings.Contains(bodyText, `"name":"kapu"`) {
		t.Fatalf("body = %s, expected name field", bodyText)
	}
	if !strings.Contains(bodyText, `"id":7`) {
		t.Fatalf("body = %s, expected id field", bodyText)
	}
}

func TestJSONClient_NewRequestAppliesAPIKeyWithoutBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "token", 3*time.Second)

	req, err := client.NewRequest(context.Background(), http.MethodGet, "/health")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if got, want := req.URL.String(), "https://example.com/health"; got != want {
		t.Fatalf("req.URL = %s, want %s", got, want)
	}
	if got, want := req.Header.Get(apiKeyHeader), "token"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}
}

func TestJSONClient_DiscardBodyClosesResponse(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "", time.Second)
	body := &trackCloseReadCloser{Reader: strings.NewReader(`{"ok":true}`)}

	if err := client.DiscardBody(&http.Response{Body: body}); err != nil {
		t.Fatalf("DiscardBody() error = %v", err)
	}
	if !body.closed {
		t.Fatal("DiscardBody() expected body close")
	}
}

func TestJSONClient_DoForwardsRequestAndReturnsResponse(t *testing.T) {
	t.Parallel()

	ts := newEchoServer(t)

	client := &JSONClient{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/ping", http.NoBody)
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:0/unreachable", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
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

	var out struct {
		Value int `json:"value"`
	}
	if err := client.DecodeJSON(resp, &out); err != nil {
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
		Body: &errorReadCloser{err: fmt.Errorf("disk full")},
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	t.Cleanup(ts.Close)
	return ts
}
