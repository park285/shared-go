package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedjson "github.com/park285/shared-go/pkg/json"
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
			if err := sharedjson.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &payload); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if payload.Error != tt.wantCode {
				t.Fatalf("error code = %q, want %q", payload.Error, tt.wantCode)
			}
		})
	}
}
