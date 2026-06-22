package obsmetrics

import (
	"bytes"
	"testing"
)

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
