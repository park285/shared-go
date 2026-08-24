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

func TestWriteCounterWithLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if !WriteCounterWithLabels(&buf, "requests_total", "request count", Labels{testStatus: "ok"}, 12) {
		t.Fatal("WriteCounterWithLabels() = false")
	}

	want := "# HELP requests_total request count\n" +
		"# TYPE requests_total counter\n" +
		"requests_total{status=\"ok\"} 12\n"
	if got := buf.String(); got != want {
		t.Fatalf("counter output = %q, want %q", got, want)
	}
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
		"# HELP requests_total request count\n",
		`requests_total{path="a\\b\"c",status="ok"} 12` + "\n",
		`requests_total{status="failed"} 3` + "\n",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("counter output missing %q: %q", want, got)
		}
	}
}

func TestWriteGaugeSeriesWritesOneFamilyHeader(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if !WriteGaugeSeries(&buf, "queue_depth", "queue depth", []GaugeSeries{
		{Labels: Labels{"lane": "primary"}, Value: "4"},
		{Labels: Labels{"lane": "secondary"}, Value: "2.5"},
	}) {
		t.Fatal("WriteGaugeSeries() = false")
	}

	got := buf.String()
	if count := bytes.Count(buf.Bytes(), []byte("# HELP queue_depth ")); count != 1 {
		t.Fatalf("HELP header count = %d, want 1; output = %q", count, got)
	}

	if count := bytes.Count(buf.Bytes(), []byte("# TYPE queue_depth gauge")); count != 1 {
		t.Fatalf("TYPE header count = %d, want 1; output = %q", count, got)
	}

	for _, want := range []string{
		`queue_depth{lane="primary"} 4` + "\n",
		`queue_depth{lane="secondary"} 2.5` + "\n",
	} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("gauge output missing %q: %q", want, got)
		}
	}
}

func TestWriteSeriesEmptyWritesHeaderOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		write func(*bytes.Buffer) bool
		want  string
	}{
		{
			name: "counter",
			write: func(buf *bytes.Buffer) bool {
				return WriteCounterSeries(buf, "requests_total", "request count", nil)
			},
			want: "# HELP requests_total request count\n# TYPE requests_total counter\n",
		},
		{
			name: "gauge",
			write: func(buf *bytes.Buffer) bool {
				return WriteGaugeSeries(buf, "queue_depth", "queue depth", nil)
			},
			want: "# HELP queue_depth queue depth\n# TYPE queue_depth gauge\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			if !testCase.write(&buf) {
				t.Fatal("series writer = false")
			}

			if got := buf.String(); got != testCase.want {
				t.Fatalf("series output = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestWriteSeriesReturnsFalseOnWriterFailure(t *testing.T) {
	t.Parallel()

	if WriteCounterSeries(failingWriter{}, "requests_total", "request count", []CounterSeries{{Value: 1}}) {
		t.Fatal("WriteCounterSeries() = true, want false")
	}

	if WriteGaugeSeries(failingWriter{}, "queue_depth", "queue depth", []GaugeSeries{{Value: "1"}}) {
		t.Fatal("WriteGaugeSeries() = true, want false")
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
