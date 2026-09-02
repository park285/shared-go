package httputil

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.StatusCode == statusCode
	}

	return false
}

func CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	const maxBodyLen = 4096

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
	if err != nil {
		_ = resp.Body.Close()

		return &APIError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("read body: %w", err),
		}
	}

	// keep-alive 재사용을 위해 에러 경로에서도 남은 body를 상한까지 비우고 닫습니다. 실패는 무시합니다.
	_ = DrainAndClose(resp.Body, DefaultDrainLimit) //nolint:errcheck // best-effort drain, 실패해도 body는 닫힌다

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

	if err := jsonv2.Unmarshal([]byte(body), &payload); err != nil {
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

func DecodeJSON[T any](resp *http.Response) (T, error) {
	return DecodeJSONLimited[T](resp, DefaultMaxBodyBytes)
}

func DecodeJSONLimited[T any](resp *http.Response, maxBytes int64) (T, error) {
	defer resp.Body.Close()

	var out T

	if maxBytes < 0 {
		maxBytes = 0
	}

	// maxBytes+1까지 읽어 본문이 상한을 실제로 넘었는지 판별한다.
	counter := &countingReader{r: io.LimitReader(resp.Body, maxBytes+1)}
	decoder := jsontext.NewDecoder(counter)
	decodeErr := jsonv2.UnmarshalDecode(decoder, &out)

	if counter.n > maxBytes {
		return out, ErrResponseBodyTooLarge
	}

	if decodeErr != nil {
		return out, decodeErr
	}

	_, trailingErr := decoder.ReadValue()

	if counter.n > maxBytes {
		return out, ErrResponseBodyTooLarge
	}

	if trailingErr == nil {
		return out, ErrMultipleJSONValues
	}

	if errors.Is(trailingErr, io.EOF) {
		return out, nil
	}

	return out, trailingErr
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
