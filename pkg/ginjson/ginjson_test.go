package ginjson_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/ginjson"
	sharedjson "github.com/park285/shared-go/pkg/json"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestRespond는 Respond가 올바른 상태코드와 JSON 바디를 기록하는지 검증한다.
func TestRespond(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ginjson.Respond(c, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("상태코드 불일치: got %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"key"`) || !strings.Contains(body, `"value"`) {
		t.Errorf("응답 바디에 기대 필드 없음: %s", body)
	}

	// 유효한 JSON인지 확인
	var out map[string]string
	if err := sharedjson.Unmarshal([]byte(strings.TrimSpace(body)), &out); err != nil {
		t.Errorf("응답 바디가 유효한 JSON이 아님: %v (body=%s)", err, body)
	}

	if out["key"] != "value" {
		t.Errorf("JSON 값 불일치: got %q, want %q", out["key"], "value")
	}
}

// TestJSON_Render는 Render가 Content-Type을 설정하고 유효한 JSON을 출력하는지 검증한다.
func TestJSON_Render(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	r := ginjson.JSON{Data: map[string]int{"count": 42}}

	err := r.Render(w)
	if err != nil {
		t.Fatalf("Render 오류: %v", err)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type 불일치: got %q, want %q", ct, "application/json; charset=utf-8")
	}

	body := strings.TrimSpace(w.Body.String())
	var out map[string]int
	if err := sharedjson.Unmarshal([]byte(body), &out); err != nil {
		t.Errorf("응답 바디가 유효한 JSON이 아님: %v (body=%s)", err, body)
	}

	if out["count"] != 42 {
		t.Errorf("JSON 값 불일치: got %d, want %d", out["count"], 42)
	}
}

// TestJSON_WriteContentType는 WriteContentType이 헤더를 설정하며
// 이미 설정된 경우 덮어쓰지 않음을 검증한다.
func TestJSON_WriteContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		preset string // 사전 설정할 Content-Type 값 (빈 문자열이면 설정 안 함)
		wantCT string
	}{
		{
			name:   "헤더 미설정 시 자동 설정",
			preset: "",
			wantCT: "application/json; charset=utf-8",
		},
		{
			name:   "헤더 기설정 시 덮어쓰지 않음",
			preset: "text/plain",
			wantCT: "text/plain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			if tc.preset != "" {
				w.Header().Set("Content-Type", tc.preset)
			}

			r := ginjson.JSON{Data: nil}
			r.WriteContentType(w)

			got := w.Header().Get("Content-Type")
			if got != tc.wantCT {
				t.Errorf("Content-Type 불일치: got %q, want %q", got, tc.wantCT)
			}
		})
	}
}

// TestRespond_DifferentStatusCodes는 Respond가 다양한 HTTP 상태코드를 올바르게 기록하는지 검증한다.
func TestRespond_DifferentStatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		data       any
	}{
		{
			name:       "201 Created",
			statusCode: http.StatusCreated,
			data:       map[string]string{"id": "123"},
		},
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			data:       map[string]string{"error": "invalid input"},
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			data:       map[string]string{"error": "internal error"},
		},
		{
			name:       "204 No Content (nil data)",
			statusCode: http.StatusNoContent,
			data:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			ginjson.Respond(c, tc.statusCode, tc.data)

			if w.Code != tc.statusCode {
				t.Errorf("상태코드 불일치: got %d, want %d", w.Code, tc.statusCode)
			}
		})
	}
}
