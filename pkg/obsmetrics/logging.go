package obsmetrics

import (
	"io"

	"github.com/park285/shared-go/v2/pkg/logging"
)

func WriteLoggingMetrics(w io.Writer) bool {
	stats := logging.SnapshotStdout()

	return WriteCounter(w, "iris_stack_logging_stdout_dropped_total",
		"Total stdout log copies dropped by admission or target write failures.", stats.Dropped) &&
		WriteCounter(w, "iris_stack_logging_stdout_truncated_total",
			"Total oversized stdout log copies successfully written as truncated JSON.", stats.Truncated) &&
		WriteCounter(w, "iris_stack_logging_file_write_failed_total",
			"Total file log writes that returned an error or short write.", logging.SnapshotFile().WriteFailed)
}
