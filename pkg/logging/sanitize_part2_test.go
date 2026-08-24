package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func credentialBranchOutput(t *testing.T, build func(*slog.Logger) *slog.Logger, attrs ...slog.Attr) string {
	t.Helper()

	var buf bytes.Buffer

	logger := slog.New(newSanitizeHandler(slog.NewTextHandler(&buf, nil)))

	if build != nil {
		logger = build(logger)
	}

	logger.LogAttrs(t.Context(), slog.LevelInfo, "credential", attrs...)

	return buf.String()
}

func TestSanitizeHandler_CredentialKeysMaskedAcrossValueBranches(t *testing.T) {
	const secret = "SECRETVALUE"

	cases := []struct {
		name  string
		build func(*slog.Logger) *slog.Logger
		attrs []slog.Attr
	}{
		{
			name:  "kind_string",
			attrs: []slog.Attr{slog.String("access_token", secret)},
		},
		{
			name:  "kind_any_map_with_privacy_key",
			attrs: []slog.Attr{slog.Any("token", map[string]any{"raw": secret, tokenUserName: "u-1"})},
		},
		{
			name:  "kind_any_map_without_privacy_key",
			attrs: []slog.Attr{slog.Any("token", map[string]any{"raw": secret})},
		},
		{
			name:  "kind_group_inline",
			attrs: []slog.Attr{slog.Group("access_token", slog.String("raw", secret))},
		},
		{
			name: "kind_group_via_with_attrs",
			build: func(l *slog.Logger) *slog.Logger {
				return l.With(slog.Group("bot_token", slog.String("raw", secret)))
			},
			attrs: []slog.Attr{slog.String("stage", "probe")},
		},
		{
			name:  "with_group_named_credential_key",
			build: func(l *slog.Logger) *slog.Logger { return l.WithGroup("access_token") },
			attrs: []slog.Attr{slog.String("raw", secret)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := credentialBranchOutput(t, tc.build, tc.attrs...)

			if strings.Contains(output, secret) {
				t.Fatalf("credential value survived masking: %s", output)
			}

			if !strings.Contains(output, redactedValue) {
				t.Fatalf("no redaction marker in output: %s", output)
			}
		})
	}
}

func TestSanitizeHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := newSanitizeHandler(baseHandler)
	loggerWithAttrs := slog.New(sanitized).With(
		slog.String("token", "should_be_masked"),
		slog.String("service", "test_service"),
	)

	loggerWithAttrs.Info("test message")

	output := buf.String()

	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected token in WithAttrs to be redacted, got: %s", output)
	}

	if strings.Contains(output, "should_be_masked") {
		t.Errorf("Expected token value to be masked, got: %s", output)
	}

	if !strings.Contains(output, "service=test_service") {
		t.Errorf("Expected service field to be preserved, got: %s", output)
	}
}

func TestSanitizeHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := newSanitizeHandler(baseHandler)
	loggerWithGroup := slog.New(sanitized).WithGroup("request")

	loggerWithGroup.Info("test",
		slog.String("api_key", "secret_key"),
		slog.String("path", "/api/users"),
	)

	output := buf.String()

	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected api_key in WithGroup to be redacted, got: %s", output)
	}

	if strings.Contains(output, "secret_key") {
		t.Errorf("Expected api_key value to be masked, got: %s", output)
	}

	if !strings.Contains(output, "path=/api/users") {
		t.Errorf("Expected path field to be preserved, got: %s", output)
	}
}

func TestSanitizeHandler_MixedScenario(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := newSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("user_login",
		slog.String("username", "alice"),
		slog.String("password", "super_secret_pass"),
		slog.String("header", "Bearer abc123.def456.ghi789"),
		slog.Int(testUserID, 42),
		slog.String(testVideoID, "dQw4w9WgXcQ"),
		slog.Group("metadata",
			slog.String("api_key", "ak_xyz"),
			slog.String("ip", "192.168.1.1"),
		),
	)

	output := buf.String()

	if !strings.Contains(output, "username=alice") {
		t.Errorf("Expected username to be preserved, got: %s", output)
	}

	if strings.Contains(output, "super_secret_pass") {
		t.Errorf("Expected password to be masked, got: %s", output)
	}

	if !strings.Contains(output, "Bearer ***REDACTED***") {
		t.Errorf("Expected Bearer token to be masked, got: %s", output)
	}

	if !strings.Contains(output, "user_id=42") {
		t.Errorf("Expected operational user_id to be preserved, got: %s", output)
	}

	if !strings.Contains(output, "video_id=dQw4w9WgXcQ") {
		t.Errorf("Expected public content id to be preserved, got: %s", output)
	}

	if strings.Contains(output, "ak_xyz") {
		t.Errorf("Expected api_key in group to be masked, got: %s", output)
	}

	if !strings.Contains(output, "ip=192.168.1.1") {
		t.Errorf("Expected ip to be preserved, got: %s", output)
	}

	redactedCount := strings.Count(output, "***REDACTED***")
	if redactedCount < 2 {
		t.Errorf("Expected at least 2 redactions (password + api_key), got %d in: %s", redactedCount, output)
	}
}

func TestSanitizeHandler_QuerySecrets(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := newSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("test", slog.String("url", "https://example.test?a=1&api_key=secret-value&token=token-value&b=2"))

	output := buf.String()

	if strings.Contains(output, "secret-value") || strings.Contains(output, "token-value") {
		t.Errorf("Expected query secrets to be masked, got: %s", output)
	}

	if !strings.Contains(output, "api_key=***REDACTED***") || !strings.Contains(output, "token=***REDACTED***") {
		t.Errorf("Expected query secret placeholders, got: %s", output)
	}
}

func TestSanitizeHandler_KeyCasePreserved(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := newSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("test", slog.String("Token", "secret123"))

	output := buf.String()

	if !strings.Contains(output, "Token=") {
		t.Errorf("Expected key 'Token' (capital T) to be preserved, got: %s", output)
	}

	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected value to be redacted, got: %s", output)
	}
}
