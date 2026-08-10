package obsmetrics

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestMetricVectorsEnforceSeriesLimit(t *testing.T) {
	t.Parallel()

	options := VecOptions{MaxSeries: 2}
	counter := NewCounterVecWithOptions("requests_total", "Requests", options)
	gauge := NewGaugeVecWithOptions("active", "Active", options)
	histogram := NewHistogramVecWithOptions("latency", "Latency", []float64{1}, options)

	for i := range 3 {
		labels := Labels{"id": fmt.Sprintf("%d", i)}
		counter.Inc(labels)
		gauge.Set(labels, float64(i))
		histogram.Observe(labels, float64(i))
	}

	for name, got := range map[string]int{
		"counter":   counter.SeriesCount(),
		"gauge":     gauge.SeriesCount(),
		"histogram": histogram.SeriesCount(),
	} {
		if got != 2 {
			t.Fatalf("%s SeriesCount() = %d, want 2", name, got)
		}
	}
	if counter.DroppedSeries() != 1 || gauge.DroppedSeries() != 1 || histogram.DroppedSeries() != 1 {
		t.Fatalf("DroppedSeries() = (%d, %d, %d), want (1, 1, 1)", counter.DroppedSeries(), gauge.DroppedSeries(), histogram.DroppedSeries())
	}
}

func TestMetricVectorRejectsOversizedLabelsBeforeRetention(t *testing.T) {
	t.Parallel()

	vec := NewCounterVecWithOptions("requests_total", "Requests", VecOptions{
		MaxSeries:          8,
		MaxLabels:          1,
		MaxLabelNameBytes:  4,
		MaxLabelValueBytes: 4,
	})
	vec.Inc(Labels{"a": "1", "b": "2"})
	vec.Inc(Labels{"long-name": "1"})
	vec.Inc(Labels{"name": strings.Repeat("x", 5)})

	if got := vec.SeriesCount(); got != 0 {
		t.Fatalf("SeriesCount() = %d, want 0", got)
	}
	if got := vec.DroppedSeries(); got != 3 {
		t.Fatalf("DroppedSeries() = %d, want 3", got)
	}
}

func TestMetricVectorConcurrentAdmissionNeverExceedsLimit(t *testing.T) {
	t.Parallel()

	const limit = 8
	vec := NewCounterVecWithOptions("requests_total", "Requests", VecOptions{MaxSeries: limit})
	var wg sync.WaitGroup
	for i := range 128 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
		}(i)
	}
	wg.Wait()

	if got := vec.SeriesCount(); got != limit {
		t.Fatalf("SeriesCount() = %d, want %d", got, limit)
	}
	if got := vec.DroppedSeries(); got != 128-limit {
		t.Fatalf("DroppedSeries() = %d, want %d", got, 128-limit)
	}
}

func TestMetricVectorCompatibilityConstructorUsesSafeDefault(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "Requests")
	for i := 0; i <= DefaultMaxMetricSeries; i++ {
		vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
	}
	if got := vec.SeriesCount(); got != DefaultMaxMetricSeries {
		t.Fatalf("SeriesCount() = %d, want %d", got, DefaultMaxMetricSeries)
	}
	if got := vec.DroppedSeries(); got != 1 {
		t.Fatalf("DroppedSeries() = %d, want 1", got)
	}
}
