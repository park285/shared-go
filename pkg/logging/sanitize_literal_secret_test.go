package logging

import (
	"strings"
	"testing"
)

func TestSanitizeKey_LiteralSecretNamesAreMasked(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"secret-key",
		tokenSecretKey,
		"api-key",
		tokenAPIKey,
		"private-key",
		tokenPrivateKey,
		"access-token",
		"access_token",
		"refresh-token",
		"refresh_token",
		" SECRET-KEY ",
		"Api_Key",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			output := keyFieldOutput(t, "key", value)
			if strings.Contains(output, value) {
				t.Fatalf("literal secret value %q leaked in output: %s", value, output)
			}
			if !strings.Contains(output, "***REDACTED***") {
				t.Fatalf("literal secret value %q was not redacted: %s", value, output)
			}
		})
	}
}
