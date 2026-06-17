package healthprobe

import (
	"bytes"
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
