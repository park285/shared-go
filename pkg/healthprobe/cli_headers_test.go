package healthprobe

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestRunMainChecksMultipleURLs(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	var out, errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, server.URL, server.URL}, &out, &errOut)

	if code != 0 {
		t.Fatalf("RunMain() = %d, want 0; stderr=%q", code, errOut.String())
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestRunMainAPIKeyEnvSendsHeader(t *testing.T) {
	t.Setenv("HEALTHCHECK_TEST_API_KEY", "secret-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret-token" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	var out, errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, "--api-key-env", "HEALTHCHECK_TEST_API_KEY", server.URL}, &out, &errOut)

	if code != 0 {
		t.Fatalf("RunMain() = %d, want 0; stderr=%q", code, errOut.String())
	}
}

func TestRunMainAPIKeyEnvRejectsEmptyValue(t *testing.T) {
	t.Setenv("HEALTHCHECK_TEST_API_KEY", " ")

	var out, errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, "--api-key-env", "HEALTHCHECK_TEST_API_KEY", "http://127.0.0.1/ready"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("RunMain() = %d, want 1", code)
	}

	if got := errOut.String(); got != "HEALTHCHECK_TEST_API_KEY is empty or not set\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunMainAPIKeyEnvRejectsBlankNames(t *testing.T) {
	for _, args := range [][]string{
		{testHealthcheck, "--api-key-env", " ", "http://127.0.0.1/ready"},
	} {
		var out, errOut bytes.Buffer

		code := RunMain(args, &out, &errOut)

		if code != 2 {
			t.Fatalf("RunMain(%v) = %d, want 2", args, code)
		}

		if !strings.Contains(errOut.String(), "must not be empty") {
			t.Fatalf("stderr = %q, want validation error", errOut.String())
		}
	}
}

func TestRunMainBodyWritesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "ready body")
	}))
	defer server.Close()

	var out, errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, "--body", server.URL}, &out, &errOut)

	if code != 0 {
		t.Fatalf("RunMain() = %d, want 0; stderr=%q", code, errOut.String())
	}

	if got := out.String(); got != "ready body" {
		t.Fatalf("stdout = %q, want body", got)
	}
}

func TestRunMainBodyAPIKeyEnvWritesResponse(t *testing.T) {
	t.Setenv("HEALTHCHECK_TEST_API_KEY", "secret-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret-token" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		testsupport.WriteResponse(t, w, "protected body")
	}))

	defer server.Close()

	var out, errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, "--body-api-key-env", "HEALTHCHECK_TEST_API_KEY", server.URL}, &out, &errOut)

	if code != 0 {
		t.Fatalf("RunMain() = %d, want 0; stderr=%q", code, errOut.String())
	}

	if got := out.String(); got != "protected body" {
		t.Fatalf("stdout = %q, want protected body", got)
	}
}

func TestRunMainReportsStdoutFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testsupport.WriteResponse(t, w, "body")
	}))
	defer server.Close()

	var errOut bytes.Buffer

	code := RunMain([]string{testHealthcheck, "--body", server.URL}, failingWriter{}, &errOut)

	if code != 1 {
		t.Fatalf("RunMain() = %d, want 1", code)
	}

	if got := errOut.String(); !strings.Contains(got, "write failed") {
		t.Fatalf("stderr = %q, want write failure", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
