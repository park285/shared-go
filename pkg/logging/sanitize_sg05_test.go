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

	const secret = "Bearer super-secret-default-logger-token"
	logger.Info("default_logger_probe", slog.String("authorization", secret))

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}

	output := string(out)
	if strings.Contains(output, "super-secret-default-logger-token") {
		t.Fatalf("NewLogger() did not redact secret, output: %s", output)
	}
	if !strings.Contains(output, "***REDACTED***") {
		t.Fatalf("NewLogger() output missing redaction marker: %s", output)
	}
}
