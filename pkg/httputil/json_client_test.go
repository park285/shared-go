package httputil

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJSONClient_NewJSONRequestSetsHeadersAndBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com/", " secret-key ", 5*time.Second)

	req, err := client.NewJSONRequest(t.Context(), http.MethodPost, "/internal/test", map[string]any{
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

	if got, want := req.Header.Get(HeaderAPIKey), "secret-key"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}

	var got map[string]any

	if err := jsonv2.Unmarshal(body, &got); err != nil {
		t.Fatalf("jsonv2.Unmarshal(body) error = %v", err)
	}

	if got["name"] != "kapu" || got["id"] != float64(7) {
		t.Fatalf("body payload = %#v, want name=kapu id=7", got)
	}
}

func TestJSONClientNewJSONRequestUsesV2WireSemantics(t *testing.T) {
	t.Parallel()

	type requestPayload struct {
		Map       map[string]int `json:"map"`
		Slice     []int          `json:"slice"`
		Count     int            `json:"count"`
		OmitZero  int            `json:"omitZero,omitzero"`
		Digest    [2]byte        `json:"digest"`
		CreatedAt time.Time      `json:"createdAt"`
	}

	client := NewJSONClient("https://example.com", "", time.Second)

	req, err := client.NewJSONRequest(t.Context(), http.MethodPost, "/v2", requestPayload{
		Digest:    [2]byte{1, 2},
		CreatedAt: time.Date(2026, time.August, 23, 1, 2, 3, 4, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewJSONRequest() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}

	const want = `{"map":{},"slice":[],"count":0,"digest":"AQI=","createdAt":"2026-08-23T01:02:03.000000004Z"}`

	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}

func TestJSONClientNewJSONRequestRejectsUnsupportedV2Shapes(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "", time.Second)
	tests := []struct {
		name    string
		payload any
	}{
		{
			name: "duration without format",
			payload: struct {
				Timeout time.Duration `json:"timeout"`
			}{Timeout: time.Second},
		},
		{
			name: "malformed struct tag",
			payload: reflect.New(reflect.StructOf([]reflect.StructField{{
				Name: "Value",
				Type: reflect.TypeFor[string](),
				Tag:  `json:"value,"`,
			}})).Elem().Interface(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.NewJSONRequest(t.Context(), http.MethodPost, "/v2", tt.payload)
			if err == nil {
				t.Fatal("NewJSONRequest() error = nil, want v2 semantic failure")
			}

			if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok {
				t.Fatalf("error type = %T, want *jsonv2.SemanticError in chain", err)
			}
		})
	}
}

func TestJSONClient_NewRequestAppliesAPIKeyWithoutBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "token", 3*time.Second)

	req, err := client.NewRequest(t.Context(), http.MethodGet, "/health")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if got, want := req.URL.String(), "https://example.com/health"; got != want {
		t.Fatalf("req.URL = %s, want %s", got, want)
	}

	if got, want := req.Header.Get(HeaderAPIKey), "token"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}
}

func TestNewJSONClientWithHTTPClientUsesProvidedClient(t *testing.T) {
	t.Parallel()

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got, want := req.URL.String(), "https://example.com/ping"; got != want {
			t.Fatalf("request URL = %s, want %s", got, want)
		}

		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := NewJSONClientWithHTTPClient("https://example.com", "token", &http.Client{Transport: rt})

	req, err := client.NewRequest(t.Context(), http.MethodGet, "/ping")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNoContent)
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
