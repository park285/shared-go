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

	if !IsStatus(err, http.StatusNotFound) {
		t.Fatal("IsStatus() = false, want true")
	}

	if IsStatus(err, http.StatusConflict) {
		t.Fatal("IsStatus() = true for wrong status")
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	rc := &trackCloseReadCloser{Reader: strings.NewReader(`{"name":"test"}`)}
	resp := &http.Response{Body: rc}

	out, err := DecodeJSON[namedPayload](resp)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}

	if out.Name != "test" {
		t.Fatalf("DecodeJSON() name = %q, want test", out.Name)
	}

	if !rc.closed {
		t.Fatal("DecodeJSON() expected body close")
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

type trackCloseReadCloser struct {
	*strings.Reader

	closed bool
}

func (t *trackCloseReadCloser) Close() error {
	t.closed = true
	return nil
}

type byteByByteReadCloser struct {
	reader    *strings.Reader
	readBytes int
	closed    bool
}

func (r *byteByByteReadCloser) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}

	n, err := r.reader.Read(p)

	r.readBytes += n

	//nolint:wrapcheck // io.Reader 계약상 io.EOF를 포함한 하위 reader의 오류를 감싸지 않고 그대로 전달해야 한다.
	return n, err
}

func (r *byteByByteReadCloser) Close() error {
	r.closed = true
	return nil
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

func TestDecodeJSON_MalformedJSON(t *testing.T) {
	t.Parallel()

	rc := &trackCloseReadCloser{Reader: strings.NewReader(`{not json`)}
	resp := &http.Response{Body: rc}

	_, err := DecodeJSON[namedPayload](resp)
	if err == nil {
		t.Fatal("DecodeJSON() expected error for malformed JSON")
	}

	if !rc.closed {
		t.Fatal("DecodeJSON() expected body close even on error")
	}
}

func TestDecodeJSONLimited_OverLimitErrors(t *testing.T) {
	t.Parallel()

	payload := `{"name":"` + strings.Repeat("x", 1024) + `"}`
	rc := &trackCloseReadCloser{Reader: strings.NewReader(payload)}
	resp := &http.Response{Body: rc}

	_, err := DecodeJSONLimited[namedPayload](resp, 64)
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("DecodeJSONLimited() error = %v, want ErrResponseBodyTooLarge", err)
	}

	if !rc.closed {
		t.Fatal("DecodeJSONLimited() expected body close on over-limit")
	}
}

func TestDecodeJSONLimited_AtLimitDecodes(t *testing.T) {
	t.Parallel()

	payload := `{"name":"test"}`
	rc := &trackCloseReadCloser{Reader: strings.NewReader(payload)}
	resp := &http.Response{Body: rc}

	out, err := DecodeJSONLimited[namedPayload](resp, int64(len(payload)))
	if err != nil {
		t.Fatalf("DecodeJSONLimited() error = %v", err)
	}

	if out.Name != "test" {
		t.Fatalf("DecodeJSONLimited() name = %q, want test", out.Name)
	}

	if !rc.closed {
		t.Fatal("DecodeJSONLimited() expected body close")
	}
}

func TestDecodeJSONLimited_TrailingWhitespaceWithinLimitDecodes(t *testing.T) {
	t.Parallel()

	payload := "{\"name\":\"test\"}\n\t "
	rc := &trackCloseReadCloser{Reader: strings.NewReader(payload)}
	resp := &http.Response{Body: rc}

	out, err := DecodeJSONLimited[namedPayload](resp, int64(len(payload)))
	if err != nil {
		t.Fatalf("DecodeJSONLimited() error = %v", err)
	}

	if out.Name != "test" {
		t.Fatalf("DecodeJSONLimited() name = %q, want test", out.Name)
	}

	if !rc.closed {
		t.Fatal("DecodeJSONLimited() expected body close")
	}
}

func TestDecodeJSONLimited_RejectsMultipleJSONValues(t *testing.T) {
	t.Parallel()

	payload := `{"name":"first"}{"name":"second"}`
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}

	_, err := DecodeJSONLimited[namedPayload](resp, int64(len(payload)))
	if !errors.Is(err, ErrMultipleJSONValues) {
		t.Fatalf("DecodeJSONLimited() error = %v, want ErrMultipleJSONValues", err)
	}
}

func TestDecodeJSONLimited_RejectsTrailingWhitespaceOverLimit(t *testing.T) {
	t.Parallel()

	jsonValue := `{"name":"test"}`
	payload := jsonValue + strings.Repeat(" ", 32)
	rc := &byteByByteReadCloser{reader: strings.NewReader(payload)}
	resp := &http.Response{Body: rc}

	_, err := DecodeJSONLimited[namedPayload](resp, int64(len(jsonValue)))
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("DecodeJSONLimited() error = %v, want ErrResponseBodyTooLarge", err)
	}

	if rc.readBytes != len(jsonValue)+1 {
		t.Fatalf("DecodeJSONLimited() read bytes = %d, want %d", rc.readBytes, len(jsonValue)+1)
	}

	if !rc.closed {
		t.Fatal("DecodeJSONLimited() expected body close")
	}
}

func TestDecodeJSON_UsesDefaultLimit(t *testing.T) {
	t.Parallel()

	if DefaultMaxBodyBytes != 16<<20 {
		t.Fatalf("DefaultMaxBodyBytes = %d, want %d", DefaultMaxBodyBytes, 16<<20)
	}

	rc := &trackCloseReadCloser{Reader: strings.NewReader(`{"name":"ok"}`)}
	resp := &http.Response{Body: rc}

	out, err := DecodeJSON[namedPayload](resp)
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}

	if out.Name != "ok" {
		t.Fatalf("DecodeJSON() name = %q, want ok", out.Name)
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

	//nolint:wrapcheck // io.Reader 계약상 io.EOF를 포함한 하위 reader의 오류를 감싸지 않고 그대로 전달해야 한다.
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

type namedPayload struct {
	Name string `json:"name"`
}
