package healthprobe

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunMainSmoke(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := RunMain([]string{testHealthcheck, "--smoke"}, &out, &errOut); code != 0 {
		t.Fatalf("smoke code = %d, want 0 (stderr=%q)", code, errOut.String())
	}

	if got := strings.TrimSpace(out.String()); got != "smoke ok" {
		t.Fatalf("smoke stdout = %q, want %q", got, "smoke ok")
	}
}

func TestRunMainUsage(t *testing.T) {
	for _, args := range [][]string{{testHealthcheck}, {testHealthcheck, "--body", "a", "b"}} {
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

	if code := RunMain([]string{testHealthcheck, server.URL}, &out, &errOut); code != 0 {
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

	if code := RunMain([]string{testHealthcheck, server.URL}, &out, &errOut); code != 1 {
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
			args:     []string{testHealthcheck, "http://service/health"},
			wantCode: 0,
		},
		{
			name:       "url failure",
			args:       []string{testHealthcheck, "http://service/health"},
			checkErr:   errors.New("service unavailable"),
			wantCode:   1,
			wantStderr: "check http://service/health: service unavailable\n",
		},
		{
			name:       "usage",
			args:       []string{testHealthcheck},
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

func TestRunChecksStartsTargetsConcurrently(t *testing.T) {
	t.Parallel()

	targets := []string{"one", "two", "three"}
	started := make(chan string, len(targets))
	release := make(chan struct{})
	done := make(chan int, 1)

	go func() {
		done <- runChecks(t.Context(), targets, nil, &bytes.Buffer{}, func(_ context.Context, target string, _ map[string]string) ([]byte, error) {
			started <- target

			<-release

			return nil, nil
		})
	}()

	seen := make(map[string]bool, len(targets))
	for range targets {
		select {
		case target := <-started:
			seen[target] = true
		case <-time.After(time.Second):
			t.Fatal("not all health targets started before release")
		}
	}

	close(release)

	if code := <-done; code != 0 {
		t.Fatalf("runChecks() = %d, want 0", code)
	}

	for _, target := range targets {
		if !seen[target] {
			t.Fatalf("target %q did not start", target)
		}
	}
}

func TestRunChecksCancelsPeersAfterFailure(t *testing.T) {
	t.Parallel()

	peerStarted := make(chan struct{})
	peerCanceled := make(chan struct{})

	var stderr bytes.Buffer

	code := runChecks(t.Context(), []string{"fail", "peer"}, nil, &stderr, func(ctx context.Context, target string, _ map[string]string) ([]byte, error) {
		if target == "peer" {
			close(peerStarted)
			<-ctx.Done()
			close(peerCanceled)

			return nil, ctx.Err()
		}

		<-peerStarted

		return nil, errors.New("probe failed")
	})

	if code != 1 {
		t.Fatalf("runChecks() = %d, want 1", code)
	}

	select {
	case <-peerCanceled:
	default:
		t.Fatal("peer fetch was not canceled before runChecks returned")
	}

	if !strings.Contains(stderr.String(), "probe failed") {
		t.Fatalf("stderr = %q, want primary failure", stderr.String())
	}
}

func TestRunChecksHonorsParentCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	done := make(chan int, 1)

	var stderr bytes.Buffer

	go func() {
		done <- runChecks(ctx, []string{"peer"}, nil, &stderr, func(ctx context.Context, _ string, _ map[string]string) ([]byte, error) {
			close(started)
			<-ctx.Done()

			return nil, ctx.Err()
		})
	}()

	<-started
	cancel()

	if code := <-done; code != 1 {
		t.Fatalf("runChecks() = %d, want 1", code)
	}

	if !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Fatalf("stderr = %q, want context cancellation", stderr.String())
	}
}
