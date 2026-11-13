package httputil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckStatus(t *testing.T) {
	t.Parallel()

	t.Run("2xx는 nil", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}
		if err := CheckStatus(resp); err != nil {
			t.Fatalf("CheckStatus() error = %v", err)
		}
	})

	t.Run("비2xx는 status/body를 포함", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("upstream failed")),
		}

		err := CheckStatus(resp)
		if err == nil {
			t.Fatal("CheckStatus() expected error")
		}

		if !strings.Contains(err.Error(), "status 502") {
			t.Fatalf("error = %q, expected status 502", err.Error())
		}

		if !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("error = %q, expected body text", err.Error())
		}
	})

	t.Run("body read 실패는 wrap", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       &errorReadCloser{err: errors.New("read fail")},
		}

		err := CheckStatus(resp)
		if err == nil {
			t.Fatal("CheckStatus() expected error")
		}

		if !strings.Contains(err.Error(), "read body") {
			t.Fatalf("error = %q, expected read body message", err.Error())
		}
	})
}

func TestCheckStatusReturnsTypedAPIError(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusConflict,
		Body: io.NopCloser(strings.NewReader(`{
			"error":"notification_in_progress",
			"message":"notification is already running",
			"request_id":"req-123",
			"details":{"trigger":"weekly"}
		}`)),
	}

	err := CheckStatus(resp)
	if err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("CheckStatus() error type = %T, want *APIError", err)
	}

	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusConflict)
	}

	if apiErr.Code != "notification_in_progress" {
		t.Fatalf("Code = %q, want notification_in_progress", apiErr.Code)
	}

	if apiErr.Message != "notification is already running" {
		t.Fatalf("Message = %q, want notification is already running", apiErr.Message)
	}

	if apiErr.RequestID != "req-123" {
		t.Fatalf("RequestID = %q, want req-123", apiErr.RequestID)
	}

	if apiErr.Details["trigger"] != "weekly" {
		t.Fatalf("Details[trigger] = %v, want weekly", apiErr.Details["trigger"])
	}
}

func TestAPIErrorHelpersMatchWrappedErrors(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("wrapped: %w", &APIError{
		StatusCode: http.StatusNotFound,
		Code:       "no_subscribed_members",
		Message:    "no subscribed members",
	})

	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("errors.AsType[*APIError]() = (%v, %t), want status 404", apiErr, ok)
	}
}

type errorReadCloser struct {
	err    error
	closed bool
}

func (e *errorReadCloser) Read(_ []byte) (int, error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	e.closed = true
	return nil
}

func TestCheckStatus_ClosesBodyOnReadFailure(t *testing.T) {
	t.Parallel()

	rc := &errorReadCloser{err: errors.New("read fail")}
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       rc,
	}

	if err := CheckStatus(resp); err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	if !rc.closed {
		t.Fatal("CheckStatus() did not close body on read failure")
	}
}

func TestAPIError_UnwrapReturnsInnerError(t *testing.T) {
	t.Parallel()

	inner := errors.New("connection refused")
	apiErr := &APIError{
		StatusCode: http.StatusBadGateway,
		Err:        inner,
	}

	got := apiErr.Unwrap()
	if !errors.Is(got, inner) {
		t.Fatalf("Unwrap() = %v, want %v", got, inner)
	}
}

func TestAPIError_UnwrapNilReceiver(t *testing.T) {
	t.Parallel()

	var apiErr *APIError

	got := apiErr.Unwrap()
	if got != nil {
		t.Fatalf("Unwrap() on nil receiver = %v, want nil", got)
	}
}

func TestAPIError_UnwrapNoInnerError(t *testing.T) {
	t.Parallel()

	apiErr := &APIError{
		StatusCode: http.StatusNotFound,
		Code:       "not_found",
	}

	got := apiErr.Unwrap()
	if got != nil {
		t.Fatalf("Unwrap() = %v, want nil", got)
	}
}

type drainTrackReadCloser struct {
	*strings.Reader

	closed    bool
	readBytes int
}

func (d *drainTrackReadCloser) Read(p []byte) (int, error) {
	n, err := d.Reader.Read(p)

	d.readBytes += n

	return n, err
}

func (d *drainTrackReadCloser) Close() error {
	d.closed = true
	return nil
}

func TestCheckStatus_DrainsAndClosesBodyOnError(t *testing.T) {
	t.Parallel()

	bodyLen := 4096 + 5000
	rc := &drainTrackReadCloser{Reader: strings.NewReader(strings.Repeat("y", bodyLen))}
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       rc,
	}

	if err := CheckStatus(resp); err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	if !rc.closed {
		t.Fatal("CheckStatus() did not close body on error")
	}

	if rc.readBytes < bodyLen {
		t.Fatalf("CheckStatus() drained %d bytes, want full body %d", rc.readBytes, bodyLen)
	}
}

func TestCheckStatus_DrainCapAndClose(t *testing.T) {
	t.Parallel()

	const drainCap = 256 * 1024

	bodyLen := 4096 + drainCap + 100000
	rc := &drainTrackReadCloser{Reader: strings.NewReader(strings.Repeat("z", bodyLen))}
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       rc,
	}

	if err := CheckStatus(resp); err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	if !rc.closed {
		t.Fatal("CheckStatus() did not close body when over drain cap")
	}

	// drain은 상한 바이트 + EOF 확인용 1바이트까지만 읽는다.
	if want := 4096 + drainCap + 1; rc.readBytes > want {
		t.Fatalf("CheckStatus() drained %d bytes, want <= %d (bounded drain + EOF probe)", rc.readBytes, want)
	}
}

func TestCheckStatus_TruncatesLargeBody(t *testing.T) {
	t.Parallel()

	largeBody := strings.Repeat("x", 8192)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(largeBody)),
	}

	err := CheckStatus(resp)
	if err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}

	if len(apiErr.Body) > 4096 {
		t.Fatalf("Body len = %d, want <= 4096", len(apiErr.Body))
	}
}
