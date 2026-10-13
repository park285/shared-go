package obsmetrics

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/logging"
)

func exerciseStdoutOutcomes(t *testing.T) logging.StdoutStats {
	t.Helper()

	// Public constructors capture os.Stdout; isolate this replacement from parallel tests.
	originalStdout := os.Stdout

	t.Cleanup(func() { os.Stdout = originalStdout })

	capture, err := os.CreateTemp(t.TempDir(), "stdout-*.log")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = capture.Close() })

	config := logging.Config{Level: "info", Dir: t.TempDir(), MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}
	before := logging.SnapshotStdout()

	os.Stdout = capture

	logger, closer, err := logging.EnableFileLoggingWithOptions(config, "metrics.log", logging.Options{AsyncStdout: true})

	os.Stdout = originalStdout

	if err != nil {
		t.Fatal(err)
	}

	logger.Error("diagnostic_failed", "error", errors.New(strings.Repeat("diagnostic detail ", 5000)))

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if closeErr := capture.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	os.Stdout = capture // A closed fd deterministically exercises synchronous stdout failure.
	logger, closer, err = logging.EnableFileLoggingWithOptions(config, "failure.log", logging.Options{})
	os.Stdout = originalStdout

	if err != nil {
		t.Fatal(err)
	}

	logger.Info("write_failed")

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	return before
}

func TestWriteLoggingMetricsExportsLiveProcessOutcomes(t *testing.T) {
	before := exerciseStdoutOutcomes(t)

	stats := logging.SnapshotStdout()
	if stats.Dropped-before.Dropped != 2 || stats.Truncated-before.Truncated != 1 {
		t.Fatalf("actual logging outcomes = %v, baseline = %v", stats, before)
	}

	var output bytes.Buffer

	if !WriteLoggingMetrics(&output) {
		t.Fatal("WriteLoggingMetrics failed")
	}

	values := map[string]uint64{
		"iris_stack_logging_stdout_dropped_total":   stats.Dropped,
		"iris_stack_logging_stdout_truncated_total": stats.Truncated,
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

	for failAt := 1; failAt <= 6; failAt++ {
		writer := &failLoggingMetricWrite{failAt: failAt}
		if WriteLoggingMetrics(writer) {
			t.Errorf("write %d failure was hidden", failAt)
		}

		if writer.calls != failAt {
			t.Errorf("writes continued after failure: calls=%d failure=%d", writer.calls, failAt)
		}
	}
}
