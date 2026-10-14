package obsmetrics

import (
	"io"

	"github.com/park285/shared-go/v2/pkg/logging"
)

// WriteLoggingMetrics는 logging.SnapshotStdout의 stdout 사본 결과와 logging.SnapshotFile의
// 파일 lane 쓰기 실패를 라벨 없는 프로세스 누적 counter로 씁니다. 두 snapshot이 다루는
// 결과만 보고하며, exposition 쓰기가 실패하면 즉시 멈추고 false를 반환합니다.
func WriteLoggingMetrics(w io.Writer) bool {
	stats := logging.SnapshotStdout()

	return WriteCounter(w, "iris_stack_logging_stdout_dropped_total",
		"Total stdout log copies dropped by admission or target write failures.", stats.Dropped) &&
		WriteCounter(w, "iris_stack_logging_stdout_truncated_total",
			"Total oversized stdout log copies successfully written as truncated JSON.", stats.Truncated) &&
		WriteCounter(w, "iris_stack_logging_file_write_failed_total",
			"Total file log writes that returned an error or short write.", logging.SnapshotFile().WriteFailed)
}
