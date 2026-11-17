package testsupport

import (
	"io"
	"testing"
)

// CloseNow는 자원을 닫고 실패하면 테스트 오류로 기록한다.
func CloseNow(tb testing.TB, name string, closeFn func() error) {
	tb.Helper()

	if err := closeFn(); err != nil {
		tb.Errorf("%s error = %v", name, err)
	}
}

// WriteResponse는 응답 문자열을 쓰고 실패하면 테스트 오류로 기록한다.
func WriteResponse(tb testing.TB, w io.Writer, body string) {
	tb.Helper()

	if _, err := io.WriteString(w, body); err != nil {
		tb.Errorf("write response error = %v", err)
	}
}

// WriteBytes는 응답 바이트를 쓰고 실패하면 테스트 오류로 기록한다.
func WriteBytes(tb testing.TB, w io.Writer, body []byte) {
	tb.Helper()

	if _, err := w.Write(body); err != nil {
		tb.Errorf("write response error = %v", err)
	}
}
