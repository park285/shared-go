package httputil

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	jsonv2 "encoding/json/v2"
	"net/http"
	"strings"
)

const (
	// ContentTypeJSON은 JSON 응답 Content-Type 값이다.
	ContentTypeJSON = "application/json"
	// HeaderContentType은 HTTP Content-Type 헤더 이름이다.
	HeaderContentType = "Content-Type"
	// HeaderAPIKey는 관리 API key 인증 헤더 이름이다.
	HeaderAPIKey = "X-API-Key" //nolint:gosec // 헤더 이름 상수이며 credential 값이 아니다.
)

// ErrorResponse는 관리 HTTP JSON 에러 응답 본문이다.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// AdminAuthConfig는 관리 API key 인증 middleware 설정이다. Disabled가 false인 zero value는 인증을 강제한다.
type AdminAuthConfig struct {
	APIKey   string
	Disabled bool
}

// ConstantTimeStringEqual은 두 문자열을 길이 차이까지 포함해 일정 시간 비교한다.
func ConstantTimeStringEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1 && len(left) == len(right)
}

// APIKeyFromRequest는 X-API-Key 또는 Bearer Authorization 값을 관리 API key로 추출한다.
func APIKeyFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get(HeaderAPIKey)); key != "" {
		return key
	}
	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// WriteJSON은 값을 JSON으로 인코딩해 HTTP 응답 본문으로 쓴다. HTML escape는 적용하지 않는다.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	var body bytes.Buffer
	if err := jsonv2.MarshalWrite(&body, v); err != nil {
		return err
	}
	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(status)
	_, err := w.Write(body.Bytes())
	return err
}

// WriteErrorJSON은 표준 관리 HTTP JSON 에러 응답을 쓴다.
func WriteErrorJSON(w http.ResponseWriter, status int, code, message string) error {
	return WriteJSON(w, status, ErrorResponse{
		Error:   strings.TrimSpace(code),
		Message: strings.TrimSpace(message),
	})
}

// AdminAuthMiddleware는 twentyq형 관리 API key 인증 middleware를 만든다.
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
