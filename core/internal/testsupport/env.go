package testsupport

import (
	"os"
	"testing"
)

func UnsetEnvOnCleanup(tb testing.TB, key string) {
	tb.Helper()

	tb.Cleanup(func() {
		if err := os.Unsetenv(key); err != nil {
			tb.Errorf("Unsetenv(%q) error = %v", key, err)
		}
	})
}
