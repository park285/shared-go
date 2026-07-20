package healthprobe

import (
	"bytes"
	"errors"
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
	for _, args := range [][]string{{"healthcheck"}, {"healthcheck", "--body", "a", "b"}} {
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

func TestRunMainExitCodesWithCheckSeam(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		checkErr   error
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:     "url success",
			args:     []string{"healthcheck", "http://service/health"},
			wantCode: 0,
		},
		{
			name:       "url failure",
			args:       []string{"healthcheck", "http://service/health"},
			checkErr:   errors.New("service unavailable"),
			wantCode:   1,
			wantStderr: "service unavailable\n",
		},
		{
			name:       "usage",
			args:       []string{"healthcheck"},
			wantCode:   2,
			wantStderr: "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]|--body <url>|--body-api-key-env <env> <url>|--smoke\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			checkCalls := 0
			code := runMain(tt.args, &out, &errOut, func(string) error {
				checkCalls++
				return tt.checkErr
			})
			if code != tt.wantCode {
				t.Fatalf("runMain() code = %d, want %d", code, tt.wantCode)
			}
			if got := out.String(); got != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", got, tt.wantStdout)
			}
			if got := errOut.String(); got != tt.wantStderr {
				t.Fatalf("stderr = %q, want %q", got, tt.wantStderr)
			}
			if len(tt.args) == 2 && tt.args[1] != "--smoke" && checkCalls != 1 {
				t.Fatalf("check calls = %d, want 1", checkCalls)
			}
		})
	}
}
