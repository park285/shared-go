//go:build unix || plan9

package obsmetrics

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/logging"
)

func exerciseLoggingOutcomes(t *testing.T) (logging.StdoutStats, logging.FileStats) {
	t.Helper()

	originalStdout := os.Stdout

	t.Cleanup(func() { os.Stdout = originalStdout })

	capture, err := os.CreateTemp(t.TempDir(), "stdout-*.log")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = capture.Close() })

	config := logging.Config{Level: "info", Dir: t.TempDir(), MaxSizeMB: 5, MaxBackups: 1, MaxAgeDays: 1}
	before := logging.SnapshotStdout()
	beforeFile := logging.SnapshotFile()

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

	config.MaxSizeMB = 1
	os.Stdout = capture
	logger, closer, err = logging.EnableFileLoggingWithOptions(config, "failure.log", logging.Options{})
	os.Stdout = originalStdout

	if err != nil {
		t.Fatal(err)
	}

	logger.Info("write_failed", "detail", strings.Repeat("oversized ", 1<<17))

	if closeErr := closer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	return before, beforeFile
}

func TestWriteLoggingMetricsExportsLiveProcessOutcomes(t *testing.T) {
	before, beforeFile := exerciseLoggingOutcomes(t)

	stats := logging.SnapshotStdout()
	if stats.Dropped-before.Dropped != 2 || stats.Truncated-before.Truncated != 1 {
		t.Fatalf("actual logging outcomes = %v, baseline = %v", stats, before)
	}

	fileStats := logging.SnapshotFile()
	if fileStats.WriteFailed-beforeFile.WriteFailed != 1 {
		t.Fatalf("actual file outcomes = %v, baseline = %v", fileStats, beforeFile)
	}

	assertLoggingMetrics(t, stats, fileStats)
}
