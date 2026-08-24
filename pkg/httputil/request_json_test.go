package httputil

import (
	jsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeJSONRequestSample struct {
	Name string `json:"name"`
}

func TestDecodeJSONRequestSuccessAndLenientUnknownFields(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"ok","extra":true}`))

	var got decodeJSONRequestSample

	if err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{}); err != nil {
		t.Fatalf("DecodeJSONRequest() error = %v", err)
	}

	if got.Name != "ok" {
		t.Fatalf("Name = %q, want ok", got.Name)
	}
}

func TestDecodeJSONRequestContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		required    bool
		wantStatus  int
	}{
		{name: "text", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "required missing", required: true, wantStatus: http.StatusUnsupportedMediaType},
		{name: "json charset", contentType: "application/json; charset=utf-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))

			if tt.contentType != "" {
				req.Header.Set(HeaderContentType, tt.contentType)
			}

			var got decodeJSONRequestSample

			err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{
				RequireContentType: tt.required,
			})

			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("DecodeJSONRequest() error = %v", err)
				}

				return
			}

			assertJSONRequestStatus(t, err, tt.wantStatus)
		})
	}
}

func TestDecodeJSONRequestStrictUnknownFields(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"ok","extra":true}`))

	var got decodeJSONRequestSample

	err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{Strict: true})
	assertJSONRequestStatus(t, err, http.StatusBadRequest)
}

func TestDecodeJSONRequestRejectsV2InvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          []byte
		strict        bool
		wantSyntactic bool
	}{
		{
			name:          "duplicate object name",
			body:          []byte(`{"name":"first","name":"second"}`),
			wantSyntactic: true,
		},
		{
			name:          "invalid UTF-8",
			body:          append([]byte(`{"name":"`), append([]byte{0xff}, []byte(`"}`)...)...),
			wantSyntactic: true,
		},
		{
			name:   "case-mismatched field",
			body:   []byte(`{"Name":"value"}`),
			strict: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(string(tt.body)))

			var got decodeJSONRequestSample

			err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{Strict: tt.strict})
			assertJSONRequestStatus(t, err, http.StatusBadRequest)

			if tt.wantSyntactic {
				if _, ok := errors.AsType[*jsontext.SyntacticError](err); !ok {
					t.Fatalf("error type = %T, want *jsontext.SyntacticError in chain", err)
				}

				return
			}

			if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok {
				t.Fatalf("error type = %T, want *jsonv2.SemanticError in chain", err)
			}
		})
	}
}

func TestDecodeJSONRequestUsesV2FixedByteArrayFormat(t *testing.T) {
	t.Parallel()

	t.Run("exact decoded length", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"digest":"AQI="}`))

		var got struct {
			Digest [2]byte `json:"digest"`
		}

		if err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{}); err != nil {
			t.Fatalf("DecodeJSONRequest() error = %v", err)
		}

		if got.Digest != [2]byte{1, 2} {
			t.Fatalf("Digest = %v, want [1 2]", got.Digest)
		}
	})

	t.Run("wrong decoded length", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"digest":"AQ=="}`))

		var got struct {
			Digest [2]byte `json:"digest"`
		}

		err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{})
		assertJSONRequestStatus(t, err, http.StatusBadRequest)

		if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok {
			t.Fatalf("error type = %T, want *jsonv2.SemanticError in chain", err)
		}
	})
}

func TestDecodeJSONRequestMalformedJSONTaxonomy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "truncated object", body: `{"name":`},
		{name: "invalid token", body: `{"name":true,}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))

			var got decodeJSONRequestSample

			err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{})
			assertJSONRequestStatus(t, err, http.StatusBadRequest)

			var requestErr *JSONRequestError

			if !errors.As(err, &requestErr) {
				t.Fatalf("error type = %T, want *JSONRequestError", err)
			}

			if requestErr.Code != "invalid_json" {
				t.Fatalf("Code = %q, want invalid_json", requestErr.Code)
			}
		})
	}
}

func TestDecodeJSONRequestRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"ok"}{"name":"extra"}`))

	var got decodeJSONRequestSample

	err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{})

	if !errors.Is(err, ErrMultipleJSONValues) {
		t.Fatalf("DecodeJSONRequest() error = %v, want ErrMultipleJSONValues", err)
	}

	assertJSONRequestStatus(t, err, http.StatusBadRequest)
}

func TestDecodeJSONRequestSizeCap(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"`+strings.Repeat("x", 64)+`"}`))

	var got decodeJSONRequestSample

	err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{MaxBodyBytes: 16})

	if !errors.Is(err, ErrRequestBodyTooLarge) {
		t.Fatalf("DecodeJSONRequest() error = %v, want ErrRequestBodyTooLarge", err)
	}

	assertJSONRequestStatus(t, err, http.StatusRequestEntityTooLarge)
}

func TestDecodeJSONRequestEmptyBody(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)

	var got decodeJSONRequestSample

	err := DecodeJSONRequest(httptest.NewRecorder(), req, &got, DecodeJSONRequestOptions{})

	if !errors.Is(err, ErrRequestBodyRequired) {
		t.Fatalf("DecodeJSONRequest() error = %v, want ErrRequestBodyRequired", err)
	}

	assertJSONRequestStatus(t, err, http.StatusBadRequest)
}

func assertJSONRequestStatus(t *testing.T, err error, want int) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want status %d", want)
	}

	var requestErr *JSONRequestError

	if !errors.As(err, &requestErr) {
		t.Fatalf("error type = %T, want *JSONRequestError", err)
	}

	if requestErr.StatusCode != want {
		t.Fatalf("StatusCode = %d, want %d", requestErr.StatusCode, want)
	}

	if got := DecodeJSONRequestStatus(err); got != want {
		t.Fatalf("DecodeJSONRequestStatus() = %d, want %d", got, want)
	}
}
