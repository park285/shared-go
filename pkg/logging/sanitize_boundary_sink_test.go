//go:build unix || plan9

package logging

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const boundaryMessage = "provider.call: provider failed token=" + redactedValue

func logBoundaryFailure(t *testing.T, logger *slog.Logger, attrs ...slog.Attr) {
	t.Helper()

	cause := &logAndWrapCodedRetryableError{
		message:   "provider failed token=" + boundarySentinel,
		code:      boundaryErrorCode,
		retryable: true,
	}
	calls := 0
	valuer := &boundaryValuer{calls: &calls, panicValue: boundarySentinel}

	attrs = append(attrs,
		slog.String(stdoutTestRequestIDKey, boundaryRequestID),
		slog.Any("response", boundaryPayload{Content: boundarySentinel}),
		slog.Any("resolved", valuer),
		slog.Any(boundarySenderKey, valuer),
		boundaryNestedGroup(9, slog.String("content", boundarySentinel)),
	)

	err := LogAndWrapError(t.Context(), logger, "provider.call", cause, attrs...)

	if !errors.Is(err, cause) {
		t.Fatal("로그 기록이 원인 오류 정체성을 변경함")
	}

	if got, ok := errors.AsType[*logAndWrapCodedRetryableError](err); !ok || got != cause {
		t.Fatal("로그 기록이 원인 오류 타입을 변경함")
	}

	if calls != 1 {
		t.Fatalf("민감 LogValuer 실행 또는 중복 해소: 호출=%d", calls)
	}
}

func assertBoundaryFailure(t *testing.T, output string) {
	t.Helper()

	if strings.Contains(output, boundarySentinel) {
		t.Fatal("sink에 원문 sentinel 노출")
	}

	record := findJSONLogRecord(t, output, boundaryMessage, 0)

	for key, want := range map[string]any{
		"event":                "provider.call.failed",
		stdoutTestRequestIDKey: boundaryRequestID,
		"error_code":           boundaryErrorCode,
		"error_type":           "logAndWrapCodedRetryableError",
		"error_message":        "provider failed token=" + redactedValue,
		"retryable":            true,
		"response":             blockedUnsupported,
		"resolved":             blockedResolution,
		boundarySenderKey:      redactedValue,
	} {
		if record[key] != want {
			t.Errorf("sink 필드 %s = %v, 기대값 %v", key, record[key], want)
		}
	}

	if !strings.Contains(output, blockedDepth) {
		t.Fatal("sink에 깊이 차단 사유 누락")
	}
}

func readBoundaryFile(t *testing.T, dir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, "boundary.log")) //nolint:gosec // 테스트 소유 임시 파일만 읽는다.
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func boundaryFileConfig(dir string) Config {
	return Config{Level: testInfo, Dir: dir, MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}
}

func TestSanitizeBoundaryRealFileAndStdout(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(strconv.FormatBool(async), func(t *testing.T) {
			dir := t.TempDir()
			before := SnapshotStdout()
			stdout := captureStdout(t, func() {
				logger, closer, err := EnableFileLoggingWithOptions(boundaryFileConfig(dir), "boundary.log", Options{AsyncStdout: async, PrivacyKeys: []string{boundarySenderKey}})
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = closer.Close() })
				logBoundaryFailure(t, logger)

				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			})
			assertBoundaryFailure(t, stdout)
			assertBoundaryFailure(t, readBoundaryFile(t, dir))

			if after := SnapshotStdout(); after != before {
				t.Fatalf("정상 출력 통계 변경: %v -> %v", before, after)
			}
		})
	}
}

func TestSanitizeBoundaryBrokenStdoutPreservesFile(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(strconv.FormatBool(async), func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = writer.Close() })

			if closeErr := reader.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}

			dir := t.TempDir()
			before := SnapshotStdout()

			logger, closer, err := enableFileLoggingWithStdout(writer, boundaryFileConfig(dir), "boundary.log", Options{AsyncStdout: async, PrivacyKeys: []string{boundarySenderKey}})
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = closer.Close() })
			logBoundaryFailure(t, logger)

			if err := closer.Close(); err != nil {
				t.Fatal(err)
			}

			file := readBoundaryFile(t, dir)
			assertBoundaryFailure(t, file)

			summary := findJSONLogRecord(t, file, "stdout lane write failed", 0)
			if summary["dropped"] != float64(2) || summary["truncated"] != float64(0) {
				t.Fatalf("실패 sink 유실 요약 = %v", summary)
			}

			after := SnapshotStdout()
			if after.Dropped-before.Dropped != 2 || after.Truncated != before.Truncated {
				t.Fatalf("실패 sink 통계 = %v -> %v", before, after)
			}
		})
	}
}

func TestSanitizeBoundarySaturatedStdoutPreservesFile(t *testing.T) {
	before := SnapshotStdout()
	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(target.release) })
	dir := t.TempDir()

	logger, closer, err := enableFileLoggingWithStdout(target, boundaryFileConfig(dir), "boundary.log", Options{AsyncStdout: true, PrivacyKeys: []string{boundarySenderKey}})
	if err != nil {
		release()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		release()

		_ = closer.Close()
	})
	<-target.started

	for range asyncStdoutQueueDepth {
		logBoundaryFailure(t, logger)
	}

	logBoundaryFailure(t, logger, slog.String("delivery", "file_only"))

	file := readBoundaryFile(t, dir)
	assertBoundaryFailure(t, file)

	if strings.Count(file, `"event":"provider.call.failed"`) != asyncStdoutQueueDepth+1 {
		t.Fatal("stdout 포화 중 파일 기록 손실")
	}

	release()

	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	stdout := target.String()
	assertBoundaryFailure(t, stdout)

	if strings.Contains(stdout, "file_only") {
		t.Fatal("포화된 stdout 큐가 추가 사본을 수용함")
	}

	after := SnapshotStdout()
	if after.Dropped-before.Dropped != 1 || after.Truncated != before.Truncated {
		t.Fatalf("포화 sink 통계 = %v -> %v", before, after)
	}

	summary := findJSONLogRecord(t, readBoundaryFile(t, dir), "stdout lane write failed", 0)
	if summary["dropped"] != float64(1) || summary["truncated"] != float64(0) {
		t.Fatalf("포화 sink 파일 요약 = %v", summary)
	}
}
