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
	counter := newCounterVecWithOptions("requests_total", "Requests", options)
	gauge := NewGaugeVecWithOptions("active", "Active", options)
	histogram := NewHistogramVecWithOptions("latency", "Latency", []float64{1}, options)

	for i := range 3 {
		labels := Labels{"id": fmt.Sprintf("%d", i)}
		counter.Inc(labels)
		gauge.Set(labels, float64(i))
		histogram.Observe(labels, float64(i))
	}

	for name, got := range map[string]int64{
		"counter":   counter.limiter.count.Load(),
		"gauge":     gauge.limiter.count.Load(),
		"histogram": histogram.limiter.count.Load(),
	} {
		if got != 2 {
			t.Fatalf("%s series count = %d, want 2", name, got)
		}
	}

	if counter.limiter.droppedSeries() != 1 || gauge.limiter.droppedSeries() != 1 || histogram.limiter.droppedSeries() != 1 {
		t.Fatalf("dropped series = (%d, %d, %d), want (1, 1, 1)", counter.limiter.droppedSeries(), gauge.limiter.droppedSeries(), histogram.limiter.droppedSeries())
	}
}

func TestMetricVectorRejectsOversizedLabelsBeforeRetention(t *testing.T) {
	t.Parallel()

	vec := newCounterVecWithOptions("requests_total", "Requests", VecOptions{
		MaxSeries:          8,
		MaxLabels:          1,
		MaxLabelNameBytes:  4,
		MaxLabelValueBytes: 4,
	})
	vec.Inc(Labels{"a": "1", "b": "2"})
	vec.Inc(Labels{"long-name": "1"})
	vec.Inc(Labels{"name": strings.Repeat("x", 5)})

	if got := vec.limiter.count.Load(); got != 0 {
		t.Fatalf("series count = %d, want 0", got)
	}

	if got := vec.limiter.droppedSeries(); got != 3 {
		t.Fatalf("dropped series = %d, want 3", got)
	}
}

func TestMetricVectorConcurrentAdmissionNeverExceedsLimit(t *testing.T) {
	t.Parallel()

	const limit = 8

	vec := newCounterVecWithOptions("requests_total", "Requests", VecOptions{MaxSeries: limit})

	var wg sync.WaitGroup

	for i := range 128 {
		wg.Go(func() {
			vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
		})
	}

	wg.Wait()

	if got := vec.limiter.count.Load(); got != limit {
		t.Fatalf("series count = %d, want %d", got, limit)
	}

	if got := vec.limiter.droppedSeries(); got != 128-limit {
		t.Fatalf("dropped series = %d, want %d", got, 128-limit)
	}
}

func TestMetricVectorCompatibilityConstructorUsesSafeDefault(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "Requests")

	for i := 0; i <= DefaultMaxMetricSeries; i++ {
		vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
	}

	if got := vec.limiter.count.Load(); got != DefaultMaxMetricSeries {
		t.Fatalf("series count = %d, want %d", got, DefaultMaxMetricSeries)
	}

	if got := vec.limiter.droppedSeries(); got != 1 {
		t.Fatalf("dropped series = %d, want 1", got)
	}
}
