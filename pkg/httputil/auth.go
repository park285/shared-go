package httputil

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
)

const (
	contentTypeJSON   = "application/json"
	headerContentType = "Content-Type"
	HeaderAPIKey      = "X-API-Key" //nolint:gosec // 헤더 이름 상수이며 credential 값이 아니다.
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type AdminAuthConfig struct {
	APIKey   string
	Disabled bool
}

func ConstantTimeStringEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))

	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1 && len(left) == len(right)
}

func APIKeyFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	return strings.TrimSpace(r.Header.Get(HeaderAPIKey))
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	var body bytes.Buffer

	if err := jsonv2.MarshalWrite(&body, v); err != nil {
		return fmt.Errorf("httputil: marshal write: %w", err)
	}

	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)

	if _, err := w.Write(body.Bytes()); err != nil {
		return fmt.Errorf("httputil: write JSON body: %w", err)
	}

	return nil
}

func WriteErrorJSON(w http.ResponseWriter, status int, code, message string) error {
	if err := WriteJSON(w, status, ErrorResponse{
		Error:   strings.TrimSpace(code),
		Message: strings.TrimSpace(message),
	}); err != nil {
		return fmt.Errorf("httputil: write JSON: %w", err)
	}

	return nil
}

func AdminAuthMiddleware(cfg AdminAuthConfig) func(http.Handler) http.Handler {
	expected := strings.TrimSpace(cfg.APIKey)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Disabled {
				next.ServeHTTP(w, r)

				return
			}

			if expected == "" {
				if err := WriteErrorJSON(w, http.StatusServiceUnavailable, "AUTH_NOT_CONFIGURED", "Admin API key not configured"); err != nil {
					return
				}

				return
			}

			provided := APIKeyFromRequest(r)
			if provided == "" || !ConstantTimeStringEqual(provided, expected) {
				if err := WriteErrorJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or missing API key"); err != nil {
					return
				}

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
