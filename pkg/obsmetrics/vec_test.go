package obsmetrics

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestCounterVecWriteExpositionSortsLabels(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "line1\nline2")
	vec.Add(Labels{
		testStatus: "200",
		testRoute:  "/v1/path",
	}, 3)

	var buf bytes.Buffer

	if !vec.WriteExposition(&buf) {
		t.Fatal("WriteExposition() = false")
	}

	body := buf.String()

	for _, want := range []string{"# HELP requests_total line1 line2", "# TYPE requests_total counter", "requests_total", "/v1/path", testStatus, " 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}

	if strings.Index(body, testRoute) > strings.Index(body, testStatus) {
		t.Fatalf("labels are not sorted by name:\n%s", body)
	}
}

func TestGaugeVecWriteExposition(t *testing.T) {
	t.Parallel()

	vec := NewGaugeVec("temperature", "Current temperature")
	vec.Set(Labels{"room": "bot"}, 3.5)

	var buf bytes.Buffer

	if !vec.WriteExposition(&buf) {
		t.Fatal("WriteExposition() = false")
	}

	body := buf.String()

	for _, want := range []string{"# HELP temperature Current temperature", "# TYPE temperature gauge", "temperature", "room", "3.5"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestHistogramVecWriteExposition(t *testing.T) {
	t.Parallel()

	vec := NewHistogramVec("latency", "Latency seconds", []float64{0.1, 1})
	labels := Labels{testProvider: "openai"}
	vec.Observe(labels, 0.05)
	vec.Observe(labels, 0.2)
	vec.Observe(labels, 2)

	var buf bytes.Buffer

	if !vec.WriteExposition(&buf) {
		t.Fatal("WriteExposition() = false")
	}

	body := buf.String()

	for _, want := range []string{"# HELP latency Latency seconds", "# TYPE latency histogram", "latency_bucket", testProvider, "le", "latency_sum", "2.25", "latency_count", " 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func BenchmarkCounterVecConcurrentCardinality(b *testing.B) {
	vec := NewCounterVec("requests_total", "Requests")

	b.ReportAllocs()
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		i := 0

		for pb.Next() {
			vec.Inc(Labels{"worker": fmt.Sprintf("w%d", i%64)})

			i++
		}
	})
}

func BenchmarkHistogramVecConcurrentCardinality(b *testing.B) {
	vec := NewHistogramVec("latency_seconds", "Latency", testLatencyBuckets)

	b.ReportAllocs()
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		i := 0

		for pb.Next() {
			vec.Observe(Labels{"worker": fmt.Sprintf("w%d", i%64)}, 0.025)

			i++
		}
	})
}
