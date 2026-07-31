package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type stallingWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *stallingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })

	<-w.release

	return len(p), nil
}

func TestAsyncDropWriterDoesNotBlockWhenTargetStalls(t *testing.T) {
	t.Parallel()

	target := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	writer := newAsyncDropWriter(target, formatText, 4)

	t.Cleanup(func() { close(target.release) })

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 32 {
			if n, err := writer.Write([]byte("line\n")); err != nil || n != 5 {
				t.Errorf("Write() = (%d, %v), want (5, nil)", n, err)

				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write() blocked while target writer stalled")
	}

	<-target.started

	if writer.droppedCount() == 0 {
		t.Fatal("droppedCount() = 0, want > 0 when queue overflows")
	}
}

func TestAsyncDropWriterDeliversAllLinesInOrderWhenTargetFast(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := newAsyncDropWriter(&buf, formatText, 64)

	var want strings.Builder

	for i := range 20 {
		line := fmt.Sprintf("line-%02d\n", i)

		want.WriteString(line)

		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := buf.String(); got != want.String() {
		t.Fatalf("delivered = %q, want %q", got, want.String())
	}

	if writer.droppedCount() != 0 {
		t.Fatalf("droppedCount() = %d, want 0", writer.droppedCount())
	}
}

func TestEnableFileLoggingWithOptionsKeepsFileLoggingWhenStdoutStalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdout := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}

	releaseStdout := sync.OnceFunc(func() { close(stdout.release) })
	t.Cleanup(releaseStdout)

	config := Config{Level: "info", Dir: dir, MaxSizeMB: 5, MaxBackups: 5, MaxAgeDays: 30, Compress: true}

	logger, closer, err := enableFileLoggingWithStdout(stdout, config, "stall.log", Options{AsyncStdout: true})
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 64 {
			logger.Info("stall_probe", "seq", i)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("logging blocked while stdout stalled")
	}

	data, err := os.ReadFile(filepath.Join(dir, "stall.log")) // #nosec G304 -- 테스트가 만든 임시 파일 읽기.
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	if got := strings.Count(string(data), "stall_probe"); got != 64 {
		t.Fatalf("file stall_probe lines = %d, want 64", got)
	}

	// stall 해제 후 동기 Close: async로 두면 closer의 마지막 파일 flush가 t.TempDir 정리와 레이스한다.
	releaseStdout()
	_ = closer.Close()
}

func TestEnableFileLoggingWithOptionsReturnsNilCloserForConsoleOnly(t *testing.T) {
	logger, closer, err := EnableFileLoggingWithOptions(Config{Level: "info"}, "unused.log", Options{AsyncStdout: true})
	if err != nil {
		t.Fatalf("EnableFileLoggingWithOptions() error = %v", err)
	}

	if logger == nil {
		t.Fatal("logger = nil, want non-nil")
	}

	if closer != nil {
		t.Fatalf("closer = %v, want nil for console-only config", closer)
	}
}
