package healthprobe

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunMainSmoke(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := RunMain([]string{"healthcheck", "--smoke"}, &out, &errOut); code != 0 {
		t.Fatalf("smoke code = %d, want 0 (stderr=%q)", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "smoke ok" {
		t.Fatalf("smoke stdout = %q, want %q", got, "smoke ok")
	}
}

func TestRunMainUsage(t *testing.T) {
	for _, args := range [][]string{{"healthcheck"}, {"healthcheck", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := RunMain(args, &out, &errOut); code != 2 {
			t.Fatalf("args %v: code = %d, want 2", args, code)
		}
		if !strings.Contains(errOut.String(), "usage:") {
			t.Fatalf("args %v: stderr = %q, want usage line", args, errOut.String())
		}
	}
}

func TestRunMainURLSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	if code := RunMain([]string{"healthcheck", server.URL}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}
}

func TestRunMainURLFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	if code := RunMain([]string{"healthcheck", server.URL}, &out, &errOut); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "status: 500") {
		t.Fatalf("stderr = %q, want status: 500", errOut.String())
	}
}
