package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var errStdoutBroken = errors.New("stdout is broken")

type brokenStdout struct {
	calls atomic.Int64
}

func (w *brokenStdout) Write(_ []byte) (int, error) {
	w.calls.Add(1)

	return 0, errStdoutBroken
}

// stdout이 죽어도 파일 lane은 계속 기록돼야 한다. Io.MultiWriter는 첫 실패 이후 나머지
// writer를 건너뛰므로, 순서만으로는 이 불변식이 지켜지지 않는다.
func TestEnableFileLogging_StdoutFailureDoesNotBlockFileLane(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	config := Config{Level: testInfo, Dir: logDir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}
	stdout := &brokenStdout{}

	logger, closer, err := enableFileLoggingWithStdout(stdout, config, "durable.log", Options{})
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}

	if closer == nil {
		t.Fatal("closer = nil, want file lane closer")
	}

	logger.Info("survives_broken_stdout", "attempt", 1)
	logger.Warn("second_line_after_failure", "attempt", 2)

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatalf("closer.Close() error = %v", closeErr)
	}

	if got := stdout.calls.Load(); got == 0 {
		t.Fatal("stdout lane was never written; test would pass vacuously")
	}

	body, err := os.ReadFile(filepath.Join(logDir, "durable.log")) //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 읽는다.
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	for _, want := range []string{"file_logging_enabled", "survives_broken_stdout", "second_line_after_failure", "stdout lane write failed"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("log file missing %q after stdout failure:\n%s", want, body)
		}
	}
}

func TestBestEffortWriter_ReportsFullWriteOnTargetError(t *testing.T) {
	t.Parallel()

	stdout := &brokenStdout{}
	payload := []byte("line\n")

	guard := &bestEffortWriter{target: stdout}

	n, err := guard.Write(payload)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil so io.MultiWriter continues", err)
	}

	// n < len(p)를 반환하면 io.MultiWriter가 ErrShortWrite로 다음 lane을 끊는다.
	if n != len(payload) {
		t.Fatalf("Write() n = %d, want %d", n, len(payload))
	}

	if stdout.calls.Load() != 1 {
		t.Fatalf("target write calls = %d, want 1", stdout.calls.Load())
	}

	if got := guard.dropped.Load(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
}

func TestBestEffortWriter_SummarizesDropsIntoFileLane(t *testing.T) {
	t.Parallel()

	var summary bytes.Buffer

	guard := &bestEffortWriter{target: &brokenStdout{}, summary: &summary}

	for range 3 {
		if _, err := guard.Write([]byte("line\n")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	if err := guard.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	first := summary.String()
	if !strings.Contains(first, "stdout lane write failed") || !strings.Contains(first, `"dropped":3`) {
		t.Fatalf("summary = %q, want one warn record with dropped=3", first)
	}

	if err := guard.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	if summary.String() != first {
		t.Fatalf("second Close emitted another summary: %q", summary.String())
	}
}

func TestBestEffortWriter_HealthyStdoutEmitsNoSummary(t *testing.T) {
	t.Parallel()

	var stdout, summary bytes.Buffer

	guard := &bestEffortWriter{target: &stdout, summary: &summary}

	if _, err := guard.Write([]byte("line\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if err := guard.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if stdout.String() != "line\n" {
		t.Fatalf("stdout = %q, want passthrough", stdout.String())
	}

	if summary.Len() != 0 {
		t.Fatalf("summary = %q, want empty when no drops", summary.String())
	}
}
