package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSG05NewLoggerRedactsSecrets_6dd85b9c(t *testing.T) {
	origStdout := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	os.Stdout = w

	t.Cleanup(func() { os.Stdout = origStdout })

	logger := NewLogger()

	const (
		secret          = "Bearer super-secret-default-logger-token"
		googleLikeKey   = "AIzaFAKEvalueNotARealGoogleApiKey00"
		githubLikeToken = "ghp_FAKEvalueNotARealGithubToken00" //nolint:gosec // 테스트 자리표시자 문자열이며 실제 자격 증명이 아니다.
	)

	logger.Info("default_logger_probe",
		slog.String("authorization", secret),
		slog.String("url", "https://maps.googleapis.com/maps/api/geocode/json?key="+googleLikeKey+"&api_key=another-secret"),
		slog.String("key", githubLikeToken),
	)

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close pipe writer: %v", closeErr)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}

	output := string(out)
	if strings.Contains(output, "super-secret-default-logger-token") {
		t.Fatalf("NewLogger() did not redact secret, output: %s", output)
	}

	if strings.Contains(output, googleLikeKey) {
		t.Fatalf("NewLogger() did not redact bare key query value, output: %s", output)
	}

	if strings.Contains(output, githubLikeToken) {
		t.Fatalf("NewLogger() did not redact secret-like literal key value, output: %s", output)
	}

	if strings.Contains(output, "another-secret") {
		t.Fatalf("NewLogger() did not redact api_key query value, output: %s", output)
	}

	if !strings.Contains(output, "***REDACTED***") {
		t.Fatalf("NewLogger() output missing redaction marker: %s", output)
	}
}
