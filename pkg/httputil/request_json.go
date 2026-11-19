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

const defaultMaxRequestBodyBytes int64 = 64 << 10

var (
	ErrRequestBodyRequired        = errors.New("httputil: request body is required")
	ErrUnsupportedJSONContentType = errors.New("httputil: content type must be application/json")
	ErrRequestBodyTooLarge        = errors.New("httputil: request body exceeds limit")
	ErrMultipleJSONValues         = errors.New("httputil: JSON body must contain a single JSON value")
)

type DecodeJSONRequestOptions struct {
	MaxBodyBytes       int64
	Strict             bool
	RequireContentType bool
}

type JSONRequestError struct {
	StatusCode int
	Code       string
	Message    string
	Err        error
}

func (e *JSONRequestError) Error() string {
	if e == nil {
		return "<nil>"
	}

	parts := []string{fmt.Sprintf("httputil: status %d", e.StatusCode)}
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

func (e *JSONRequestError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func DecodeJSONRequestStatus(err error) int {
	if requestErr, ok := errors.AsType[*JSONRequestError](err); ok {
		return requestErr.StatusCode
	}

	return http.StatusBadRequest
}

func DecodeJSONRequest(w http.ResponseWriter, r *http.Request, v any, opts DecodeJSONRequestOptions) error {
	if r == nil || r.Body == nil {
		return newJSONRequestError(
			http.StatusBadRequest,
			"invalid_request",
			"request body is required",
			ErrRequestBodyRequired,
		)
	}

	if err := validateJSONRequestContentType(r.Header.Get(headerContentType), opts.RequireContentType); err != nil {
		return err
	}

	maxBytes := opts.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxRequestBodyBytes
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
	if err != nil || mediaType != contentTypeJSON {
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
