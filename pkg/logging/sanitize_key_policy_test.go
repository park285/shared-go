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

func TestSanitizeHandlerStructuredGroupCredentialsUseExactOrValueEvidence(t *testing.T) {
	t.Parallel()

	nested := []slog.Attr{
		slog.String(genericKeyField, accessTokenPlaceholder),
		slog.String(testUserID, "user-42"),
		slog.Int("room_id", 8842),
	}
	payload := slog.Group("payload",
		slog.String("api_key", "short-secret"),
		slog.String(genericKeyField, "cache-shard-1"),
		slog.Attr{Key: testNested, Value: slog.GroupValue(nested...)},
	)
	output := privacyOutput(t, payload)

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

	if payload.Value.Group()[0].Value.String() != "short-secret" ||
		nested[0].Value.String() != accessTokenPlaceholder || nested[1].Value.String() != "user-42" ||
		nested[2].Value.Int64() != 8842 {
		t.Fatal("호출자 group 변경")
	}
}

func TestSanitizeHandlerOperationalIDsRemainObservable(t *testing.T) {
	t.Parallel()

	output := privacyOutput(t,
		slog.String(testUserID, "user-42"),
		slog.Int("room_id", 8842),
	)
	if strings.Contains(output, redactedValue) {
		t.Fatalf("operational correlation IDs were masked: %s", output)
	}

	if !strings.Contains(output, "user_id=user-42") || !strings.Contains(output, "room_id=8842") {
		t.Fatalf("operational correlation IDs were not preserved: %s", output)
	}
}
