package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeKey_SecretLikeValueMasked_8e92058d(t *testing.T) {
	secretValues := []string{
		"sk_live_" + "FAKEvalueNotARealStripeKey",
		"sk_test_" + "FAKEvalueNotARealStripeKey",
		"ghp_" + "FAKEvalueNotARealGithubToken00",
		"github_pat_" + "FAKEvalueNotARealGithubPat0000",
		"AKIA" + "FAKEEXAMPLEAWSKEY000",
		"AIza" + "FAKEvalueNotARealGoogleApiKey00",
		"xoxb-" + "FAKE-value-not-a-real-slack-token",
		"eyJ" + "FAKEjwtHeader.FAKEjwtBody.FAKEsignature",
		"aB3dE7fG9hJ2kL5mN8pQ1rS4tU6vW0xYzAbCdEfGhIj",
	}
	for _, v := range secretValues {
		out := keyFieldOutput(t, "key", v)
		if strings.Contains(out, v) {
			t.Errorf("secret-like value under key=%q not masked, got: %s", v, out)
		}
		if !strings.Contains(out, "***REDACTED***") {
			t.Errorf("expected ***REDACTED*** for secret-like value %q, got: %s", v, out)
		}
	}
}

func TestSanitizeKey_IdentifierValueNotMasked_8e92058d(t *testing.T) {
	identifierValues := []string{
		"member:news:rooms",
		"member:news:room_names",
		"notification:egress:lease",
		"session:index:ghost",
		"official_schedule_page",
		"user-1234",
		"lock-abc",
		"apikey123",
		"42",
		"en-US",
		"hololive",
		"/api/v1/cache",
	}
	for _, v := range identifierValues {
		out := keyFieldOutput(t, "key", v)
		if strings.Contains(out, "***REDACTED***") {
			t.Errorf("plain identifier value %q under key= must NOT be masked, got: %s", v, out)
		}
		if !strings.Contains(out, v) {
			t.Errorf("plain identifier value %q must be preserved, got: %s", v, out)
		}
	}
}

func keyFieldOutput(t *testing.T, key, value string) string {
	t.Helper()
	var buf bytes.Buffer
	h := NewSanitizeHandler(slog.NewTextHandler(&buf, nil))
	slog.New(h).Info("m", slog.String(key, value))
	return buf.String()
}
