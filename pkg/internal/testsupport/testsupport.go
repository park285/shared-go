package testsupport

import (
	"io"
	"log"
	"log/slog"
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
