package httputil

import (
	"bytes"
	jsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConstantTimeStringEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "same", left: "secret", right: "secret", want: true},
		{name: "case differs", left: "secret", right: "Secret", want: false},
		{name: "length differs", left: "secret", right: "secret ", want: false},
		{name: "empty same", left: "", right: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ConstantTimeStringEqual(tt.left, tt.right); got != tt.want {
				t.Fatalf("ConstantTimeStringEqual(%q, %q) = %t, want %t", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestAPIKeyFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		apiKeyHeader  string
		authorization string
		want          string
	}{
		{name: "x api key", apiKeyHeader: " secret ", want: "secret"},
		{name: "bearer fallback", authorization: "Bearer token ", want: "token"},
		{name: "x api key wins", apiKeyHeader: "key", authorization: "Bearer token", want: "key"},
		{name: "bearer prefix case sensitive", authorization: "bearer token", want: ""},
		{name: "missing", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if tt.apiKeyHeader != "" {
				req.Header.Set(HeaderAPIKey, tt.apiKeyHeader)
			}
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			if got := APIKeyFromRequest(req); got != tt.want {
				t.Fatalf("APIKeyFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := WriteJSON(rec, http.StatusCreated, map[string]string{"html": "<b>&</b>"})
	if err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get(HeaderContentType); got != ContentTypeJSON {
		t.Fatalf("Content-Type = %q, want %q", got, ContentTypeJSON)
	}
	body := rec.Body.String()
	if body != `{"html":"<b>&</b>"}` {
		t.Fatalf("body = %q, want unescaped HTML JSON", body)
	}
}

func TestWriteJSONEncodeFailureDoesNotCommitResponse(t *testing.T) {
	t.Parallel()

	w := &responseWriteProbe{header: make(http.Header)}
	err := WriteJSON(w, http.StatusCreated, map[string]string{"invalid": "ok\xffbad"})
	if err == nil {
		t.Fatal("WriteJSON() error = nil, want invalid UTF-8 failure")
	}
	var syntacticErr *jsontext.SyntacticError
	if !errors.As(err, &syntacticErr) {
		t.Fatalf("error type = %T, want *jsontext.SyntacticError", err)
	}
	if w.status != 0 || w.body.Len() != 0 || w.header.Get(HeaderContentType) != "" {
		t.Fatalf("response committed on encode failure: status=%d header=%q body=%q", w.status, w.header.Get(HeaderContentType), w.body.String())
	}
}

func TestWriteErrorJSONTrimsAndUsesWriteJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if err := WriteErrorJSON(rec, http.StatusBadRequest, "  CODE  ", "  msg  "); err != nil {
		t.Fatalf("WriteErrorJSON() error = %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get(HeaderContentType); got != ContentTypeJSON {
		t.Fatalf("Content-Type = %q, want %q", got, ContentTypeJSON)
	}
	var payload ErrorResponse
	if err := jsonv2.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &payload); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if payload.Error != "CODE" || payload.Message != "msg" {
		t.Fatalf("payload = %+v, want trimmed CODE/msg", payload)
	}
}

func TestAdminAuthMiddleware(t *testing.T) {
	t.Parallel()

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		cfg        AdminAuthConfig
		apiKey     string
		authHeader string
		wantStatus int
		wantCode   string
		wantCalled bool
	}{
		{
			name:       "zero value empty secret fails closed",
			cfg:        AdminAuthConfig{},
			apiKey:     "provided",
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "AUTH_NOT_CONFIGURED",
		},
		{
			name:       "disabled auth allows all",
			cfg:        AdminAuthConfig{Disabled: true},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "valid key via x api key",
			cfg:        AdminAuthConfig{APIKey: "secret"},
			apiKey:     "secret",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "valid key via bearer",
			cfg:        AdminAuthConfig{APIKey: "secret"},
			authHeader: "Bearer secret",
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "invalid key",
			cfg:        AdminAuthConfig{APIKey: "secret"},
			apiKey:     "wrong",
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name:       "missing key",
			cfg:        AdminAuthConfig{APIKey: "secret"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := AdminAuthMiddleware(tt.cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				okHandler.ServeHTTP(w, r)
			}))
			req := httptest.NewRequest(http.MethodGet, "/admin/test", http.NoBody)
			if tt.apiKey != "" {
				req.Header.Set(HeaderAPIKey, tt.apiKey)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if called != tt.wantCalled {
				t.Fatalf("next called = %t, want %t", called, tt.wantCalled)
			}
			if tt.wantCode == "" {
				return
			}

			var payload ErrorResponse
			if err := jsonv2.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &payload); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if payload.Error != tt.wantCode {
				t.Fatalf("error code = %q, want %q", payload.Error, tt.wantCode)
			}
		})
	}
}

type responseWriteProbe struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *responseWriteProbe) Header() http.Header { return w.header }

func (w *responseWriteProbe) WriteHeader(status int) { w.status = status }

func (w *responseWriteProbe) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}
