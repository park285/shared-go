package testsupport

import (
	"io"
	"os"
	"testing"
)

func CloseNow(tb testing.TB, name string, closeFn func() error) {
	tb.Helper()

	if err := closeFn(); err != nil {
		tb.Errorf("%s error = %v", name, err)
	}
}

func CloseOnCleanup(tb testing.TB, name string, closeFn func() error) {
	tb.Helper()

	tb.Cleanup(func() {
		if err := closeFn(); err != nil {
			tb.Errorf("%s error = %v", name, err)
		}
	})
}

func WriteResponse(tb testing.TB, w io.Writer, body string) {
	tb.Helper()

	if _, err := io.WriteString(w, body); err != nil {
		tb.Errorf("write response error = %v", err)
	}
}

func WriteBytes(tb testing.TB, w io.Writer, body []byte) {
	tb.Helper()

	if _, err := w.Write(body); err != nil {
		tb.Errorf("write response error = %v", err)
	}
}

func AssertType[T any](tb testing.TB, name string, value any) T {
	tb.Helper()

	typed, ok := value.(T)
	if !ok {
		tb.Fatalf("%s type = %T, want %T", name, value, typed)
	}

	return typed
}

func UnsetEnvOnCleanup(tb testing.TB, key string) {
	tb.Helper()

	tb.Cleanup(func() {
		if err := os.Unsetenv(key); err != nil {
			tb.Errorf("Unsetenv(%q) error = %v", key, err)
		}
	})
}
