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

func IsStatus(err error, statusCode int) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == statusCode
	}
	return false
}

func CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	const maxBodyLen = 4096
	// keep-alive 재사용을 위해 에러 경로에서도 남은 body를 상한까지 비우고 닫습니다.
	const maxDrainLen = 256 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
	if err != nil {
		_ = resp.Body.Close()
		return &APIError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("read body: %w", err),
		}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainLen)) //nolint:errcheck // best-effort drain, 실패해도 아래 Close로 정리
	_ = resp.Body.Close()
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

const DefaultMaxBodyBytes int64 = 16 << 20

var ErrResponseBodyTooLarge = errors.New("httputil: response body exceeds limit")

func DecodeJSON(resp *http.Response, v any) error {
	return DecodeJSONLimited(resp, v, DefaultMaxBodyBytes)
}

func DecodeJSONLimited(resp *http.Response, v any, maxBytes int64) error {
	defer func() { _ = resp.Body.Close() }()
	if maxBytes < 0 {
		maxBytes = 0
	}
	// maxBytes+1까지 읽어 본문이 상한을 실제로 넘었는지 판별한다.
	counter := &countingReader{r: io.LimitReader(resp.Body, maxBytes+1)}
	decoder := sharedjson.NewDecoder(counter)
	decodeErr := decoder.Decode(v)
	if counter.n > maxBytes {
		return ErrResponseBodyTooLarge
	}
	if decodeErr != nil {
		//nolint:wrapcheck // 호출부에서 컨텍스트 추가
		return decodeErr
	}

	var extra any
	trailingErr := decoder.Decode(&extra)
	if counter.n > maxBytes {
		return ErrResponseBodyTooLarge
	}
	if trailingErr == nil {
		return ErrMultipleJSONValues
	}
	if errors.Is(trailingErr, io.EOF) {
		return nil
	}
	//nolint:wrapcheck // 호출부에서 컨텍스트 추가
	return trailingErr
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
