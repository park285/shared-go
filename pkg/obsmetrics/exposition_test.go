package obsmetrics

import (
	"bytes"
	"testing"
)

func TestWriteCounterWithLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if !WriteCounterWithLabels(&buf, "requests_total", "request count", Labels{"status": "ok"}, 12) {
		t.Fatal("WriteCounterWithLabels() = false")
	}

	want := "# HELP requests_total request count\n" +
		"# TYPE requests_total counter\n" +
		"requests_total{status=\"ok\"} 12\n"
	if got := buf.String(); got != want {
		t.Fatalf("counter output = %q, want %q", got, want)
	}
}

func TestWriteHistogramRejectsMalformedSnapshot(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ok := WriteHistogram(&buf, "bad_histogram", "bad snapshot", HistogramSnapshot{
		UpperBounds: []float64{0.1, 1},
		Cumulative:  []uint64{3},
		Total:       3,
	})
	if ok {
		t.Fatal("WriteHistogram() = true, want false for mismatched histogram slices")
	}
}
