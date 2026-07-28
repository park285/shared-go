package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactDiagnosticMasksCredentialForms(t *testing.T) {
	t.Parallel()

	canaries := []string{
		"bearer-secret",
		"query-secret",
		"userinfo-secret",
		"env-secret",
		"json-secret",
		"dsn-secret",
		"colon-secret",
		"wrapped-colon-secret",
		"wrapped-equals-secret",
	}
	raw := "Authorization: Bearer bearer-secret " +
		"https://example.test/path?token=query-secret " +
		"postgres://user:userinfo-secret@example.test/db " +
		"API_TOKEN=env-secret " +
		`{"api_key":"json-secret"} ` +
		"password='dsn-secret' " +
		"client_secret: colon-secret " +
		"load config: password: wrapped-colon-secret " +
		"failed: API_TOKEN=wrapped-equals-secret"

	got := RedactDiagnostic(raw)
	for _, canary := range canaries {
		if strings.Contains(got, canary) {
			t.Fatalf("RedactDiagnostic() leaked %q in %q", canary, got)
		}
	}
	if strings.Count(got, "***REDACTED***") < len(canaries) {
		t.Fatalf("RedactDiagnostic() redaction count = %d, want at least %d in %q", strings.Count(got, "***REDACTED***"), len(canaries), got)
	}
}

func TestRedactDiagnosticLeavesCleanTextUnchanged(t *testing.T) {
	t.Parallel()

	const clean = "connect example.test:4317: connection refused"
	if got := RedactDiagnostic(clean); got != clean {
		t.Fatalf("RedactDiagnostic() = %q, want %q", got, clean)
	}
}

func TestSanitizeHandlerRedactsErrorValues(t *testing.T) {
	t.Parallel()

	const canary = "error-secret"
	var output bytes.Buffer
	logger := slog.New(newSanitizeHandler(slog.NewTextHandler(&output, nil)))
	logger.Error("startup failed", slog.Any("error", errors.New("API_TOKEN="+canary)))

	got := output.String()
	if strings.Contains(got, canary) {
		t.Fatalf("sanitize handler leaked error credential: %q", got)
	}
	if !strings.Contains(got, "***REDACTED***") {
		t.Fatalf("sanitize handler output = %q, want redaction marker", got)
	}
}

func TestSanitizeHandlerFullyMasksErrorUnderSensitiveKey(t *testing.T) {
	t.Parallel()

	const canary = "opaque-error-secret"
	var output bytes.Buffer
	logger := slog.New(newSanitizeHandler(slog.NewTextHandler(&output, nil)))
	logger.Error("startup failed", slog.Any("password", errors.New(canary)))

	got := output.String()
	if strings.Contains(got, canary) {
		t.Fatalf("sanitize handler leaked sensitive error attr: %q", got)
	}
	if !strings.Contains(got, "***REDACTED***") {
		t.Fatalf("sanitize handler output = %q, want redaction marker", got)
	}
}
