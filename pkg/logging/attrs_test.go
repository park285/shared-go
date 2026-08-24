package logging

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

type codedRetryableError struct {
	msg       string
	code      string
	retryable bool
}

func (e *codedRetryableError) Error() string   { return e.msg }
func (e *codedRetryableError) Code() string    { return e.code }
func (e *codedRetryableError) Retryable() bool { return e.retryable }

func attrsToMap(attrs []slog.Attr) map[string]slog.Value {
	m := make(map[string]slog.Value, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value
	}

	return m
}

func TestErrorAttrs_NilReturnsNil(t *testing.T) {
	if got := ErrorAttrs(nil); got != nil {
		t.Fatalf("ErrorAttrs(nil) = %v, want nil", got)
	}
}

func TestErrorAttrs_PlainError(t *testing.T) {
	m := attrsToMap(ErrorAttrs(errors.New("boom")))
	if got := m["error_message"].String(); got != "boom" {
		t.Fatalf("error_message = %q, want boom", got)
	}

	if _, ok := m["error_type"]; !ok {
		t.Fatal("error_type missing")
	}

	if _, ok := m["error_code"]; ok {
		t.Fatal("error_code present for plain error, want absent")
	}

	if _, ok := m["retryable"]; ok {
		t.Fatal("retryable present for plain error, want absent")
	}
}

func TestErrorAttrs_CodedRetryable(t *testing.T) {
	cause := &codedRetryableError{msg: "temporary", code: "TEMP", retryable: true}
	m := attrsToMap(ErrorAttrs(cause))

	if got := m["error_code"].String(); got != "TEMP" {
		t.Fatalf("error_code = %q, want TEMP", got)
	}

	v, ok := m["retryable"]
	if !ok {
		t.Fatal("retryable missing")
	}

	if v.Kind() != slog.KindBool || !v.Bool() {
		t.Fatalf("retryable = %v, want true", v)
	}
}

func TestErrorAttrs_EmptyCodeOmitted(t *testing.T) {
	cause := &codedRetryableError{msg: "x", code: "", retryable: false}
	m := attrsToMap(ErrorAttrs(cause))

	if _, ok := m["error_code"]; ok {
		t.Fatal("error_code present for empty code, want omitted")
	}

	v, ok := m["retryable"]
	if !ok {
		t.Fatal("retryable missing for retryable error with false value")
	}

	if v.Bool() {
		t.Fatal("retryable = true, want false")
	}
}

func TestErrorAttrs_ProbesWrappedChain(t *testing.T) {
	cause := &codedRetryableError{msg: "temporary", code: "TEMP", retryable: true}
	wrapped := fmt.Errorf("layer: %w", cause)
	m := attrsToMap(ErrorAttrs(wrapped))

	if got := m["error_message"].String(); got != "layer: temporary" {
		t.Fatalf("error_message = %q, want layer: temporary", got)
	}

	if got := m["error_code"].String(); got != "TEMP" {
		t.Fatalf("error_code = %q, want TEMP (probed through wrap)", got)
	}

	v, ok := m["retryable"]
	if !ok || !v.Bool() {
		t.Fatalf("retryable = %v, want true (probed through wrap)", v)
	}
}

func TestErrorType_NamedTypeUsesName(t *testing.T) {
	if got := errorType(&codedRetryableError{}); got != "codedRetryableError" {
		t.Fatalf("errorType = %q, want codedRetryableError", got)
	}
}

func TestErrorType_StringFallbackForPlain(t *testing.T) {
	if got := errorType(errors.New("x")); got == "" {
		t.Fatal("errorType = empty, want non-empty type string")
	}
}
