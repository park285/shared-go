package obsmetrics

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/logging"
)

func TestWriteLoggingMetricsExportsCurrentCounters(t *testing.T) {
	assertLoggingMetrics(t, logging.SnapshotStdout(), logging.SnapshotFile())
}

func assertLoggingMetrics(t *testing.T, stats logging.StdoutStats, fileStats logging.FileStats) {
	t.Helper()

	var output bytes.Buffer

	if !WriteLoggingMetrics(&output) {
		t.Fatal("WriteLoggingMetrics failed")
	}

	values := map[string]uint64{
		"iris_stack_logging_stdout_dropped_total":    stats.Dropped,
		"iris_stack_logging_stdout_truncated_total":  stats.Truncated,
		"iris_stack_logging_file_write_failed_total": fileStats.WriteFailed,
	}
	for name, value := range values {
		if !strings.Contains(output.String(), "# TYPE "+name+" counter\n") {
			t.Errorf("missing counter declaration for %s", name)
		}

		want := name + " " + strconv.FormatUint(value, 10) + "\n"
		if strings.Count(output.String(), want) != 1 {
			t.Errorf("missing or duplicated unlabelled sample %q in %q", want, output.String())
		}
	}

	if got := logging.SnapshotStdout(); got != stats {
		t.Fatalf("scraping changed monotonic counters: %v -> %v", stats, got)
	}

	if got := logging.SnapshotFile(); got != fileStats {
		t.Fatalf("scraping changed monotonic file counters: %v -> %v", fileStats, got)
	}
}

type failLoggingMetricWrite struct {
	calls  int
	failAt int
}

func (w *failLoggingMetricWrite) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, errors.New("metrics connection closed")
	}

	return len(p), nil
}

func TestWriteLoggingMetricsStopsOnExpositionFailure(t *testing.T) {
	t.Parallel()

	complete := &failLoggingMetricWrite{}
	if !WriteLoggingMetrics(complete) {
		t.Fatal("WriteLoggingMetrics() = false without write failure")
	}

	for failAt := 1; failAt <= complete.calls; failAt++ {
		writer := &failLoggingMetricWrite{failAt: failAt}
		if WriteLoggingMetrics(writer) {
			t.Errorf("write %d failure was hidden", failAt)
		}

		if writer.calls != failAt {
			t.Errorf("writes continued after failure: calls=%d failure=%d", writer.calls, failAt)
		}
	}
}
