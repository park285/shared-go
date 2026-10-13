package obsmetrics

import (
	"io"

	"github.com/park285/shared-go/v2/pkg/logging"
)

// WriteLoggingMetrics writes process-lifetime stdout-copy outcome counters without
// labels. It reports only outcomes covered by logging.SnapshotStdout and returns
// false on an exposition write failure.
func WriteLoggingMetrics(w io.Writer) bool {
	stats := logging.SnapshotStdout()

	return WriteCounter(w, "iris_stack_logging_stdout_dropped_total",
		"Total stdout log copies dropped by admission or target write failures.", stats.Dropped) &&
		WriteCounter(w, "iris_stack_logging_stdout_truncated_total",
			"Total oversized stdout log copies successfully written as truncated JSON.", stats.Truncated)
}
