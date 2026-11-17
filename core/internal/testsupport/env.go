package testsupport

import (
	"os"
	"testing"
)

// UnsetEnvOnCleanup은 테스트 종료 시 지정한 환경 변수를 지운다.
func UnsetEnvOnCleanup(tb testing.TB, key string) {
	tb.Helper()

	tb.Cleanup(func() {
		if err := os.Unsetenv(key); err != nil {
			tb.Errorf("Unsetenv(%q) error = %v", key, err)
		}
	})
}
