package httputil

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultMaxRequestBodyBytes는 JSON 요청 본문의 기본 최대 크기다.
const DefaultMaxRequestBodyBytes int64 = 64 << 10

var (
	// ErrRequestBodyRequired는 필수 JSON 요청 본문이 없을 때의 오류다.
	ErrRequestBodyRequired = errors.New("httputil: request body is required")
	// ErrUnsupportedJSONContentType는 JSON Content-Type이 아닐 때의 오류다.
	ErrUnsupportedJSONContentType = errors.New("httputil: content type must be application/json")
	// ErrRequestBodyTooLarge는 JSON 요청 본문이 허용 크기를 넘을 때의 오류다.
	ErrRequestBodyTooLarge = errors.New("httputil: request body exceeds limit")
	// ErrMultipleJSONValues는 JSON 본문에 값이 둘 이상 있을 때의 오류다.
	ErrMultipleJSONValues = errors.New("httputil: JSON body must contain a single JSON value")
)

// DecodeJSONRequestOptions는 JSON 요청 decode 동작을 조정한다.
type DecodeJSONRequestOptions struct {
	// MaxBodyBytes는 요청 본문 최대 크기다.
	MaxBodyBytes int64
	// Strict는 알 수 없는 JSON field를 거부한다.
	Strict bool
	// RequireContentType은 application/json Content-Type을 필수로 한다.
	RequireContentType bool
}

// JSONRequestError는 JSON 요청 decode 실패의 HTTP taxonomy다.
type JSONRequestError struct {
	// StatusCode는 응답해야 할 HTTP status code다.
	StatusCode int
	// Code는 machine-readable 오류 code다.
	Code string
	// Message는 응답에 사용할 오류 message다.
	Message string
	// Err는 원래 decode 또는 validation 오류다.
	Err error
}

// Error는 JSON 요청 오류를 문자열로 반환한다.
func (e *JSONRequestError) Error() string {
	if e == nil {
		return "<nil>"
	}

	parts := []string{fmt.Sprintf("status %d", e.StatusCode)}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}

	if e.Message != "" {
		parts = append(parts, e.Message)
	}

	if e.Err != nil && e.Err.Error() != e.Message {
		parts = append(parts, e.Err.Error())
	}

	return strings.Join(parts, ": ")
}

// Unwrap은 원래 JSON 요청 decode 오류를 반환한다.
func (e *JSONRequestError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// DecodeJSONRequestStatus는 JSON 요청 오류에 대응하는 HTTP status를 반환한다.
func DecodeJSONRequestStatus(err error) int {
	if requestErr, ok := errors.AsType[*JSONRequestError](err); ok {
		return requestErr.StatusCode
	}

	return http.StatusBadRequest
}

// DecodeJSONRequest는 HTTP 요청 본문에서 단일 JSON 값을 decode한다.
func DecodeJSONRequest(w http.ResponseWriter, r *http.Request, v any, opts DecodeJSONRequestOptions) error {
	if r == nil || r.Body == nil {
		return newJSONRequestError(
			http.StatusBadRequest,
			"invalid_request",
			"request body is required",
			ErrRequestBodyRequired,
		)
	}

	if err := validateJSONRequestContentType(r.Header.Get(HeaderContentType), opts.RequireContentType); err != nil {
		return fmt.Errorf("validate JSON request content type: %w", err)
	}

	maxBytes := opts.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBodyBytes
	}

	body := http.MaxBytesReader(w, r.Body, maxBytes)

	defer func() { _ = body.Close() }()

	counter := &jsonRequestBodyReader{r: body}
	dec := jsontext.NewDecoder(counter)

	var decodeOptions jsonv2.Options

	if opts.Strict {
		decodeOptions = jsonv2.RejectUnknownMembers(true)
	}

	if err := jsonv2.UnmarshalDecode(dec, v, decodeOptions); err != nil {
		return mapJSONRequestDecodeError(err, counter.sawNonSpace)
	}

	if _, err := dec.ReadValue(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}

		return mapJSONRequestDecodeError(err, counter.sawNonSpace)
	}

	return newJSONRequestError(
		http.StatusBadRequest,
		"invalid_json",
		"request body must contain a single JSON value",
		ErrMultipleJSONValues,
	)
}

func validateJSONRequestContentType(contentType string, required bool) error {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" && !required {
		return nil
	}

	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil || mediaType != ContentTypeJSON {
		return newJSONRequestError(
			http.StatusUnsupportedMediaType,
			"unsupported_media_type",
			"content type must be application/json",
			ErrUnsupportedJSONContentType,
		)
	}

	return nil
}

func mapJSONRequestDecodeError(err error, sawBody bool) *JSONRequestError {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return newJSONRequestError(
			http.StatusRequestEntityTooLarge,
			"request_entity_too_large",
			"request body exceeds limit",
			fmt.Errorf("%w: %w", ErrRequestBodyTooLarge, err),
		)
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return newJSONRequestError(http.StatusBadRequest, "invalid_json", err.Error(), err)
	}

	if errors.Is(err, io.EOF) {
		if sawBody {
			return newJSONRequestError(http.StatusBadRequest, "invalid_json", err.Error(), err)
		}

		return newJSONRequestError(
			http.StatusBadRequest,
			"invalid_request",
			"request body is required",
			ErrRequestBodyRequired,
		)
	}

	return newJSONRequestError(http.StatusBadRequest, "invalid_json", err.Error(), err)
}

type jsonRequestBodyReader struct {
	r           io.Reader
	sawNonSpace bool
}

func (r *jsonRequestBodyReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	for _, b := range p[:n] {
		switch b {
		case ' ', '\n', '\r', '\t':
		default:
			r.sawNonSpace = true

			return n, err
		}
	}

	return n, err
}

func newJSONRequestError(status int, code, message string, err error) *JSONRequestError {
	return &JSONRequestError{
		StatusCode: status,
		Code:       code,
		Message:    message,
		Err:        err,
	}
}
