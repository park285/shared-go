package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeHandler_SensitiveKeys(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		value          string
		expectRedacted bool
	}{
		{"token lowercase", "token", "secret123", true},
		{"Token uppercase", "Token", "secret123", true},
		{"TOKEN all caps", "TOKEN", "secret123", true},
		{"password", "password", "mypass", true},
		{"Password", "Password", "mypass", true},
		{"secret", "secret", "topsecret", true},
		{"key", "key", "apikey123", true},
		{"authorization", "authorization", "auth_value", true},
		{"cookie", "cookie", "session=xyz", true},
		{"api_key", "api_key", "ak_12345", true},
		{"bot_token suffix", "bot_token", "bot-secret", true},
		{"client_secret", "client_secret", "client-secret", true},
		{"webhook_url", "webhook_url", "https://example.test/hook", true},
		{"apikey", "apikey", "ak_12345", true},
		{"ApiKey mixed", "ApiKey", "ak_12345", true},
		{"non-sensitive", "username", "john_doe", false},
		{"non-sensitive number", "count", "42", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			baseHandler := slog.NewTextHandler(&buf, nil)
			sanitized := NewSanitizeHandler(baseHandler)
			logger := slog.New(sanitized)

			logger.Info("test", slog.String(tt.key, tt.value))
			output := buf.String()

			if tt.expectRedacted {
				if !strings.Contains(output, "***REDACTED***") {
					t.Errorf("Expected ***REDACTED*** in output, got: %s", output)
				}
				if strings.Contains(output, tt.value) {
					t.Errorf("Expected value %q to be masked, but found in output: %s", tt.value, output)
				}
				if !strings.Contains(output, tt.key+"=") {
					t.Errorf("Expected key %q to be preserved in output, got: %s", tt.key, output)
				}
			} else {
				if strings.Contains(output, "***REDACTED***") {
					t.Errorf("Did not expect redaction for key %q, got: %s", tt.key, output)
				}
				if !strings.Contains(output, tt.value) {
					t.Errorf("Expected value %q in output, got: %s", tt.value, output)
				}
			}
		})
	}
}

func TestSanitizeHandler_BearerToken(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			"Bearer with alphanumeric",
			"Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature",
			"Bearer ***REDACTED***",
		},
		{
			"Bearer with dots and dashes",
			"Bearer abc-123_def.ghi",
			"Bearer ***REDACTED***",
		},
		{
			"Multiple Bearer tokens",
			"Bearer token1 and Bearer token2",
			"Bearer ***REDACTED*** and Bearer ***REDACTED***",
		},
		{
			"No Bearer token",
			"Basic dXNlcjpwYXNz",
			"Basic dXNlcjpwYXNz",
		},
		{
			"Bearer lowercase matched",
			"bearer token123",
			"bearer ***REDACTED***",
		},
		{
			"Bearer mixed case matched",
			"bEaReR token123",
			"bEaReR ***REDACTED***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			baseHandler := slog.NewTextHandler(&buf, nil)
			sanitized := NewSanitizeHandler(baseHandler)
			logger := slog.New(sanitized)

			logger.Info("auth", slog.String("header", tt.value))
			output := buf.String()

			if !strings.Contains(output, tt.expected) {
				t.Errorf("Expected %q in output, got: %s", tt.expected, output)
			}
		})
	}
}

func TestSanitizeHandler_GroupHandling(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := NewSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("test",
		slog.Group("auth",
			slog.String("password", "secret123"),
			slog.String("username", "john"),
		),
	)
	output := buf.String()

	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected group password to be redacted, got: %s", output)
	}
	if strings.Contains(output, "secret123") {
		t.Errorf("Expected password value to be masked, got: %s", output)
	}
	if !strings.Contains(output, "username=john") {
		t.Errorf("Expected username to be preserved, got: %s", output)
	}
}

func TestSanitizeHandler_NestedGroups(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := NewSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("test",
		slog.Group("outer",
			slog.Group("inner",
				slog.String("secret", "nested_secret"),
				slog.String("public", "visible"),
			),
		),
	)
	output := buf.String()

	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected nested secret to be redacted, got: %s", output)
	}
	if strings.Contains(output, "nested_secret") {
		t.Errorf("Expected nested secret value to be masked, got: %s", output)
	}
	if !strings.Contains(output, "public=visible") {
		t.Errorf("Expected public field to be preserved, got: %s", output)
	}
}

func TestSanitizeHandler_NonStringValues(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := NewSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("test",
		slog.Int("token", 12345),
		slog.Bool("password", true),
		slog.Float64("secret", 3.14),
	)
	output := buf.String()

	if strings.Contains(output, "***REDACTED***") {
		t.Errorf("Non-string values should not be redacted, got: %s", output)
	}
	if !strings.Contains(output, "12345") {
		t.Errorf("Expected int value to be preserved, got: %s", output)
	}
	if !strings.Contains(output, "true") {
		t.Errorf("Expected bool value to be preserved, got: %s", output)
	}
	if !strings.Contains(output, "3.14") {
		t.Errorf("Expected float value to be preserved, got: %s", output)
	}
}

func TestSanitizeHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	sanitized := NewSanitizeHandler(baseHandler)
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
	sanitized := NewSanitizeHandler(baseHandler)
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
	sanitized := NewSanitizeHandler(baseHandler)
	logger := slog.New(sanitized)

	logger.Info("user_login",
		slog.String("username", "alice"),
		slog.String("password", "super_secret_pass"),
		slog.String("header", "Bearer abc123.def456.ghi789"),
		slog.Int("user_id", 42),
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
		t.Errorf("Expected user_id to be preserved, got: %s", output)
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
	sanitized := NewSanitizeHandler(baseHandler)
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
	sanitized := NewSanitizeHandler(baseHandler)
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
