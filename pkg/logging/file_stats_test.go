package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableFileLoggingCountsFileLaneWriteFailures(t *testing.T) {
	// 프로세스 counter의 정확한 증가분을 보므로 package의 parallel test와 분리한다.
	beforeFile := SnapshotFile()
	beforeStdout := SnapshotStdout()

	dir := t.TempDir()
	config := Config{Level: testInfo, Dir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}

	var stdout bytes.Buffer

	logger, closer, err := enableFileLoggingWithStdout(&stdout, config, "file.log", Options{})
	if err != nil {
		t.Fatal(err)
	}

	// lumberjack은 MaxSizeMB보다 큰 단일 쓰기를 (0, error)로 거절한다.
	logger.Info("oversized_record", "detail", strings.Repeat("oversized ", 1<<17))
	logger.Info("survivor")

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if got := SnapshotFile().WriteFailed - beforeFile.WriteFailed; got != 1 {
		t.Fatalf("file write failures delta = %d, want 1", got)
	}

	if got := SnapshotStdout(); got != beforeStdout {
		t.Fatalf("file failure changed stdout counters: %v -> %v", beforeStdout, got)
	}

	file, err := os.ReadFile(filepath.Join(dir, "file.log")) //nolint:gosec // 테스트 소유 임시 파일이다.
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(file), "survivor") || strings.Contains(string(file), "oversized_record") {
		t.Fatalf("file lane kept wrong records:\n%s", file)
	}

	for _, want := range []string{"oversized_record", "survivor"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout copy missing %q", want)
		}
	}
}
