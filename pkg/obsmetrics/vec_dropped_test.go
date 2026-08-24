package obsmetrics

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

func expositionLines(t *testing.T, write func(w io.Writer) bool) []string {
	t.Helper()

	var buf bytes.Buffer

	if !write(&buf) {
		t.Fatal("WriteExposition() = false")
	}

	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func assertHasLine(t *testing.T, lines []string, want string) {
	t.Helper()

	if slices.Contains(lines, want) {
		return
	}

	t.Fatalf("exposition missing line %q:\n%s", want, strings.Join(lines, "\n"))
}

func TestWriteExposition_EmitsDroppedSeriesCounter(t *testing.T) {
	t.Parallel()

	t.Run("counter", func(t *testing.T) {
		t.Parallel()

		vec := NewCounterVecWithOptions("requests_total", "Requests", VecOptions{MaxSeries: 1})
		vec.Inc(Labels{testRoute: "/a"})
		vec.Inc(Labels{testRoute: "/b"})
		vec.Inc(Labels{testRoute: "/c"})

		lines := expositionLines(t, vec.WriteExposition)
		assertHasLine(t, lines, "# TYPE requests_total_dropped_series_total counter")
		assertHasLine(t, lines, "requests_total_dropped_series_total 2")

		if got := vec.DroppedSeries(); got != 2 {
			t.Fatalf("DroppedSeries() = %d, want 2", got)
		}
	})

	t.Run("gauge", func(t *testing.T) {
		t.Parallel()

		vec := NewGaugeVecWithOptions("temperature", "Temp", VecOptions{MaxSeries: 1})
		vec.Set(Labels{"room": "a"}, 1)
		vec.Set(Labels{"room": "b"}, 2)

		lines := expositionLines(t, vec.WriteExposition)
		assertHasLine(t, lines, "temperature_dropped_series_total 1")
	})

	t.Run("histogram", func(t *testing.T) {
		t.Parallel()

		vec := NewHistogramVecWithOptions("latency", "Latency", []float64{1}, VecOptions{MaxSeries: 1})
		vec.Observe(Labels{testProvider: "a"}, 0.5)
		vec.Observe(Labels{testProvider: "b"}, 0.5)

		lines := expositionLines(t, vec.WriteExposition)
		assertHasLine(t, lines, "latency_dropped_series_total 1")
	})
}

// 드롭이 0일 때도 series를 내보내야 "0"과 "메트릭 없음"이 구분된다.
func TestWriteExposition_EmitsZeroDroppedSeries(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "Requests")
	vec.Inc(Labels{testRoute: "/a"})

	lines := expositionLines(t, vec.WriteExposition)
	assertHasLine(t, lines, "requests_total_dropped_series_total 0")
}

func TestWriteExposition_DroppedFamilyHasSingleHeaderPair(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "Requests")
	vec.Inc(Labels{testRoute: "/a"})
	vec.Inc(Labels{testRoute: "/b"})

	lines := expositionLines(t, vec.WriteExposition)
	help, typ := 0, 0

	for _, line := range lines {
		if strings.HasPrefix(line, "# HELP requests_total_dropped_series_total ") {
			help++
		}

		if strings.HasPrefix(line, "# TYPE requests_total_dropped_series_total ") {
			typ++
		}
	}

	if help != 1 || typ != 1 {
		t.Fatalf("dropped family headers help=%d type=%d, want 1/1:\n%s", help, typ, strings.Join(lines, "\n"))
	}
}

func TestWriteExposition_NilVecStaysSilent(t *testing.T) {
	t.Parallel()

	var (
		counter   *CounterVec
		gauge     *GaugeVec
		histogram *HistogramVec
	)

	for name, write := range map[string]func(w io.Writer) bool{
		"counter":   func(w io.Writer) bool { return counter.WriteExposition(w) },
		"gauge":     func(w io.Writer) bool { return gauge.WriteExposition(w) },
		"histogram": func(w io.Writer) bool { return histogram.WriteExposition(w) },
	} {
		var buf bytes.Buffer

		if !write(&buf) {
			t.Fatalf("%s: nil vec WriteExposition() = false", name)
		}

		if buf.Len() != 0 {
			t.Fatalf("%s: nil vec wrote %q, want empty", name, buf.String())
		}
	}
}

func TestCanonicalLabels_LengthPrefixPreventsKeyCollision(t *testing.T) {
	t.Parallel()

	left, _ := canonicalLabels(Labels{"a": "b=1:c", "d": "e"})
	right, _ := canonicalLabels(Labels{"a": "b", "d": "e", "1": "c"})

	if left == right {
		t.Fatalf("distinct label sets collided on key %q", left)
	}

	empty, pairs := canonicalLabels(nil)
	if empty != "" || pairs != nil {
		t.Fatalf("canonicalLabels(nil) = (%q, %v), want (\"\", nil)", empty, pairs)
	}
}

func TestCanonicalLabels_StableAndOrderIndependent(t *testing.T) {
	t.Parallel()

	first, pairs := canonicalLabels(Labels{"zeta": "1", "alpha": "2", "mid": "3"})
	second, _ := canonicalLabels(Labels{"mid": "3", "alpha": "2", "zeta": "1"})

	if first != second {
		t.Fatalf("key differs by map order: %q vs %q", first, second)
	}

	if want := "5:alpha=1:2;3:mid=1:3;4:zeta=1:1;"; first != want {
		t.Fatalf("canonical key = %q, want %q", first, want)
	}

	if len(pairs) != 3 || pairs[0].name != "alpha" || pairs[2].name != "zeta" {
		t.Fatalf("pairs = %v, want sorted by name", pairs)
	}
}

func TestCanonicalLabels_LongValueLengthPrefix(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("v", 200)
	key, _ := canonicalLabels(Labels{"k": value})

	if want := fmt.Sprintf("1:k=%d:%s;", len(value), value); key != want {
		t.Fatalf("canonical key = %q, want %q", key, want)
	}
}

func BenchmarkCanonicalLabels(b *testing.B) {
	labels := Labels{
		testRoute:    "/v1/messages",
		testStatus:   "200",
		testProvider: "openai",
		"lane":       "command",
	}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = canonicalLabels(labels)
	}
}

func BenchmarkEscapeLabelValue(b *testing.B) {
	values := []string{"/v1/messages", `has "quote"`, "line\nbreak", `back\slash`}

	b.ReportAllocs()

	i := 0

	for b.Loop() {
		_ = escapeLabelValue(values[i%len(values)])
		i++
	}
}
