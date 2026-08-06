package logging

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeHandlerGenericKeyRequiresSecretValueEvidence(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"cache-shard-1", "partition-primary", "apikey123"} {
		output := privacyOutput(t, slog.String("key", value))
		if strings.Contains(output, redactedValue) {
			t.Fatalf("ordinary key value %q was masked: %s", value, output)
		}
		if !strings.Contains(output, "key="+value) {
			t.Fatalf("ordinary key value %q was not preserved: %s", value, output)
		}
	}

	for _, value := range []string{"access-token", "Aa0" + strings.Repeat("x", 21)} {
		output := privacyOutput(t, slog.String("key", value))
		if strings.Contains(output, value) || !strings.Contains(output, redactedValue) {
			t.Fatalf("secret-like key value %q was not masked: %s", value, output)
		}
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
