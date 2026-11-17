package logging

import (
	"bytes"
	"log/slog"
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
	record := slog.NewRecord(testTime(), slog.LevelError, "oversized_record", 0)
	record.AddAttrs(
		slog.String("detail", strings.Repeat("oversized ", 1<<17)),
		slog.String(stdoutTestRequestIDKey, boundaryRequestID),
		slog.String("error_code", boundaryErrorCode),
		slog.Any("response", boundaryPayload{Content: boundarySentinel}),
	)

	if handleErr := logger.Handler().Handle(t.Context(), record); handleErr == nil {
		t.Fatal("파일 쓰기 실패가 Handler 오류로 전달되지 않음")
	}

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

	if strings.Contains(stdout.String(), boundarySentinel) || strings.Contains(string(file), boundarySentinel) {
		t.Fatal("파일 오류 경로에서 원문 sentinel 노출")
	}

	delivered := findJSONLogRecord(t, stdout.String(), "oversized_record", 0)
	if delivered["response"] != blockedUnsupported || delivered[stdoutTestRequestIDKey] != boundaryRequestID || delivered["error_code"] != boundaryErrorCode {
		t.Fatal("파일 쓰기 실패가 stdout 차단 사유 또는 식별자를 제거함")
	}
}
