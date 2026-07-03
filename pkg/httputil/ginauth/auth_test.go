package ginauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	sharedjson "github.com/park285/shared-go/pkg/json"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAPIKeyAuthMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiKey     string
		headerVal  string
		wantStatus int
		wantError  string
		wantMsg    string
	}{
		{name: "empty api key allows", wantStatus: http.StatusOK},
		{name: "missing key", apiKey: "test-key", wantStatus: http.StatusUnauthorized, wantError: "unauthorized", wantMsg: "API key required"},
		{name: "wrong key", apiKey: "test-key", headerVal: "wrong-key", wantStatus: http.StatusForbidden, wantError: "forbidden", wantMsg: "invalid API key"},
		{name: "valid key", apiKey: "test-key", headerVal: "test-key", wantStatus: http.StatusOK},
		{name: "spaced key remains invalid", apiKey: "test-key", headerVal: " test-key ", wantStatus: http.StatusForbidden, wantError: "forbidden", wantMsg: "invalid API key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.Use(APIKeyAuthMiddleware(tt.apiKey))
			router.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set(APIKeyHeader, tt.headerVal)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assertGinAuthResponse(t, rec, tt.wantStatus, tt.wantError, tt.wantMsg)
		})
	}
}

func TestNoRouteAuthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiKey     string
		headerVal  string
		wantStatus int
		wantError  string
		wantMsg    string
	}{
		{name: "empty api key no route", wantStatus: http.StatusNotFound, wantError: "not_found", wantMsg: "endpoint not found"},
		{name: "missing key", apiKey: "test-key", wantStatus: http.StatusUnauthorized, wantError: "unauthorized", wantMsg: "API key required"},
		{name: "wrong key", apiKey: "test-key", headerVal: "wrong-key", wantStatus: http.StatusForbidden, wantError: "forbidden", wantMsg: "invalid API key"},
		{name: "valid key still no route", apiKey: "test-key", headerVal: "test-key", wantStatus: http.StatusNotFound, wantError: "not_found", wantMsg: "endpoint not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.NoRoute(NoRouteAuthHandler(tt.apiKey))

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/missing", http.NoBody)
			if tt.headerVal != "" {
				req.Header.Set(APIKeyHeader, tt.headerVal)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assertGinAuthResponse(t, rec, tt.wantStatus, tt.wantError, tt.wantMsg)
		})
	}
}

func assertGinAuthResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantError, wantMsg string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d", rec.Code, wantStatus)
	}
	if wantError == "" {
		return
	}

	var payload map[string]any
	if err := sharedjson.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := payload["error"]; got != wantError {
		t.Fatalf("error = %v, want %q", got, wantError)
	}
	if got := payload["message"]; got != wantMsg {
		t.Fatalf("message = %v, want %q", got, wantMsg)
	}
}
