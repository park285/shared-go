package httputil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	sharedjson "github.com/park285/shared-go/pkg/json"
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Details    map[string]any
	Body       string
	Err        error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{fmt.Sprintf("status %d", e.StatusCode)}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" && e.Message != e.Code {
		parts = append(parts, e.Message)
	}
	if e.Body != "" && e.Code == "" && e.Message == "" {
		parts = append(parts, e.Body)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

func IsStatus(err error, statusCode int) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.StatusCode == statusCode
}

func CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	const maxBodyLen = 4096
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
	if err != nil {
		return &APIError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("read body: %w", err),
		}
	}
	return newAPIError(resp.StatusCode, strings.TrimSpace(string(body)))
}

type errorResponse struct {
	Error     string         `json:"error"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details"`
	Success   *bool          `json:"success"`
}

func newAPIError(statusCode int, body string) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Body:       body,
	}
	if body == "" {
		return apiErr
	}

	var payload errorResponse
	if err := sharedjson.Unmarshal([]byte(body), &payload); err != nil {
		return apiErr
	}
	apiErr.Code = strings.TrimSpace(payload.Error)
	apiErr.Message = strings.TrimSpace(payload.Message)
	apiErr.RequestID = strings.TrimSpace(payload.RequestID)
	apiErr.Details = payload.Details
	return apiErr
}

func DecodeJSON(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	//nolint:wrapcheck // 호출부에서 컨텍스트 추가
	return sharedjson.NewDecoder(resp.Body).Decode(v)
}
