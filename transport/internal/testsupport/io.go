package testsupport

import (
	"io"
	"testing"
)

func CloseNow(tb testing.TB, name string, closeFn func() error) {
	tb.Helper()

	if err := closeFn(); err != nil {
		tb.Errorf("%s error = %v", name, err)
	}
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
