package obsmetrics

import (
	"bytes"
	"errors"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteCounterSeriesWritesOneFamilyHeader(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if !WriteCounterSeries(&buf, "requests_total", "request\ncount", []CounterSeries{
		{Labels: Labels{testStatus: "ok", "path": `a\b"c`}, Value: 12},
		{Labels: Labels{testStatus: "failed"}, Value: 3},
	}) {
		t.Fatal("WriteCounterSeries() = false")
	}

	got := buf.String()
	if count := bytes.Count(buf.Bytes(), []byte("# HELP requests_total ")); count != 1 {
		t.Fatalf("HELP header count = %d, want 1; output = %q", count, got)
	}

	if count := bytes.Count(buf.Bytes(), []byte("# TYPE requests_total counter")); count != 1 {
		t.Fatalf("TYPE header count = %d, want 1; output = %q", count, got)
	}

	for _, want := range []string{
		"# HELP requests_total request\\ncount\n",
		`requests_total{path="a\\b\"c",status="ok"} 12` + "\n",
		`requests_total{status="failed"} 3` + "\n",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("counter output missing %q: %q", want, got)
		}
	}
}

func TestWriteCounterSeriesEmptyWritesHeaderOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if !WriteCounterSeries(&buf, "requests_total", "request count", nil) {
		t.Fatal("WriteCounterSeries() = false")
	}

	if got, want := buf.String(), "# HELP requests_total request count\n# TYPE requests_total counter\n"; got != want {
		t.Fatalf("series output = %q, want %q", got, want)
	}
}

func TestWriteSeriesReturnsFalseOnWriterFailure(t *testing.T) {
	t.Parallel()

	if WriteCounterSeries(failingWriter{}, "requests_total", "request count", []CounterSeries{{Value: 1}}) {
		t.Fatal("WriteCounterSeries() = true, want false")
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
