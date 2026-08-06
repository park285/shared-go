package logging

import (
	"log/slog"
	"strings"
	"testing"
)

const (
	genericKeyField        = "key"
	accessTokenPlaceholder = "access-token"
)

func TestSanitizeHandlerGenericKeyRequiresSecretValueEvidence(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"cache-shard-1", "partition-primary", "apikey123"} {
		output := privacyOutput(t, slog.String(genericKeyField, value))
		if strings.Contains(output, redactedValue) {
			t.Fatalf("ordinary key value %q was masked: %s", value, output)
		}
		if !strings.Contains(output, genericKeyField+"="+value) {
			t.Fatalf("ordinary key value %q was not preserved: %s", value, output)
		}
	}

	for _, value := range []string{accessTokenPlaceholder, "Aa0" + strings.Repeat("x", 21)} {
		output := privacyOutput(t, slog.String(genericKeyField, value))
		if strings.Contains(output, value) || !strings.Contains(output, redactedValue) {
			t.Fatalf("secret-like key value %q was not masked: %s", value, output)
		}
	}
}

func TestSanitizeHandlerStructuredMapCredentialsUseExactOrValueEvidence(t *testing.T) {
	t.Parallel()

	nested := map[string]any{
		genericKeyField: accessTokenPlaceholder,
		"user_id":       "user-42",
		"room_id":       8842,
	}
	payload := map[string]any{
		"api_key":       "short-secret",
		genericKeyField: "cache-shard-1",
		"nested":        nested,
	}
	output := privacyOutput(t, slog.Any("payload", payload))

	for _, leaked := range []string{"short-secret", accessTokenPlaceholder} {
		if strings.Contains(output, leaked) {
			t.Fatalf("structured credential %q leaked: %s", leaked, output)
		}
	}
	if strings.Count(output, redactedValue) < 2 {
		t.Fatalf("structured credentials were not independently masked: %s", output)
	}
	for _, preserved := range []string{"cache-shard-1", "user-42", "8842"} {
		if !strings.Contains(output, preserved) {
			t.Fatalf("ordinary structured value %q was not preserved: %s", preserved, output)
		}
	}
	if payload["api_key"] != "short-secret" || payload[genericKeyField] != "cache-shard-1" ||
		nested[genericKeyField] != accessTokenPlaceholder || nested["user_id"] != "user-42" || nested["room_id"] != 8842 {
		t.Fatalf("caller-owned structured map was mutated: payload=%v nested=%v", payload, nested)
	}
}

func TestSanitizeHandlerOperationalIDsRemainObservable(t *testing.T) {
	t.Parallel()

	output := privacyOutput(t,
		slog.String("user_id", "user-42"),
		slog.Int("room_id", 8842),
	)
	if strings.Contains(output, redactedValue) {
		t.Fatalf("operational correlation IDs were masked: %s", output)
	}
	if !strings.Contains(output, "user_id=user-42") || !strings.Contains(output, "room_id=8842") {
		t.Fatalf("operational correlation IDs were not preserved: %s", output)
	}
}
