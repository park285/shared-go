package testsupport

import (
	"io"
	"log"
	"log/slog"
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

// UseDefaultLogger는 테스트 동안 slog.Default()를 handler로 바꾸고 cleanup에서 되돌린다.
// 표준 log 출력도 slog.SetDefault가 handler로 돌리므로 그 출력과 flag까지 복원해야 이후 테스트 출력이
// 이 handler로 새지 않는다. 전역 상태를 바꾸므로 t.Parallel 테스트에서 부르지 않는다.
func UseDefaultLogger(tb testing.TB, handler slog.Handler) {
	tb.Helper()

	previous := slog.Default()
	previousOutput, previousFlags := log.Writer(), log.Flags()

	slog.SetDefault(slog.New(handler))
	tb.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
}
