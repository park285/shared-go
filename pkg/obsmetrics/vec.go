package obsmetrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Labels는 Prometheus label name/value 집합입니다. 렌더링 시 label name 기준으로 정렬됩니다.
type Labels map[string]string

type labelPair struct {
	name  string
	value string
}

type seriesEntry[T any] struct {
	key    string
	labels []labelPair
	value  T
}

// CounterVec는 동일 metric name/help를 공유하는 label-set별 counter 집합입니다.
type CounterVec struct {
	name   string
	help   string
	series sync.Map // map[string]*seriesEntry[*Counter]
}

// Counter는 원자적으로 증가하는 단일 counter series입니다.
type Counter struct {
	value atomic.Uint64
}

func NewCounterVec(name, help string) *CounterVec {
	return &CounterVec{name: name, help: help}
}

func (v *CounterVec) With(labels Labels) *Counter {
	if v == nil {
		return nil
	}

	key, labelPairs := canonicalLabels(labels)
	entry := &seriesEntry[*Counter]{
		key:    key,
		labels: labelPairs,
		value:  &Counter{},
	}
	actual, _ := v.series.LoadOrStore(key, entry)
	stored, ok := actual.(*seriesEntry[*Counter])
	if !ok {
		return nil
	}

	return stored.value
}

func (v *CounterVec) Inc(labels Labels) {
	v.Add(labels, 1)
}

func (v *CounterVec) Add(labels Labels, delta uint64) {
	counter := v.With(labels)
	if counter != nil {
		counter.Add(delta)
	}
}

func (v *CounterVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "counter") {
		return false
	}

	for _, entry := range collectSeries[*Counter](&v.series) {
		if !writeMetricSample(w, v.name, entry.labels, strconv.FormatUint(entry.value.Value(), 10)) {
			return false
		}
	}

	return true
}

func (c *Counter) Inc() {
	c.Add(1)
}

func (c *Counter) Add(delta uint64) {
	if c != nil {
		c.value.Add(delta)
	}
}

func (c *Counter) Value() uint64 {
	if c == nil {
		return 0
	}

	return c.value.Load()
}

// GaugeVec는 동일 metric name/help를 공유하는 label-set별 float64 gauge 집합입니다.
type GaugeVec struct {
	name   string
	help   string
	series sync.Map // map[string]*seriesEntry[*Gauge]
}

// Gauge는 원자적으로 설정되는 단일 gauge series입니다.
type Gauge struct {
	bits atomic.Uint64
}

func NewGaugeVec(name, help string) *GaugeVec {
	return &GaugeVec{name: name, help: help}
}

func (v *GaugeVec) With(labels Labels) *Gauge {
	if v == nil {
		return nil
	}

	key, labelPairs := canonicalLabels(labels)
	entry := &seriesEntry[*Gauge]{
		key:    key,
		labels: labelPairs,
		value:  &Gauge{},
	}
	actual, _ := v.series.LoadOrStore(key, entry)
	stored, ok := actual.(*seriesEntry[*Gauge])
	if !ok {
		return nil
	}

	return stored.value
}

func (v *GaugeVec) Set(labels Labels, value float64) {
	gauge := v.With(labels)
	if gauge != nil {
		gauge.Set(value)
	}
}

func (v *GaugeVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "gauge") {
		return false
	}

	for _, entry := range collectSeries[*Gauge](&v.series) {
		if !writeMetricSample(w, v.name, entry.labels, strconv.FormatFloat(entry.value.Value(), 'g', -1, 64)) {
			return false
		}
	}

	return true
}

func (g *Gauge) Set(value float64) {
	if g != nil {
		g.bits.Store(math.Float64bits(value))
	}
}

func (g *Gauge) Value() float64 {
	if g == nil {
		return 0
	}

	return math.Float64frombits(g.bits.Load())
}

// HistogramVec는 동일 metric name/help/bucket을 공유하는 label-set별 histogram 집합입니다.
type HistogramVec struct {
	name    string
	help    string
	buckets []float64
	series  sync.Map // map[string]*seriesEntry[*Histogram]
}

func NewHistogramVec(name, help string, buckets []float64) *HistogramVec {
	bounds := make([]float64, len(buckets))
	copy(bounds, buckets)

	return &HistogramVec{name: name, help: help, buckets: bounds}
}

func (v *HistogramVec) With(labels Labels) *Histogram {
	if v == nil {
		return nil
	}

	key, labelPairs := canonicalLabels(labels)
	entry := &seriesEntry[*Histogram]{
		key:    key,
		labels: labelPairs,
		value:  NewHistogram(v.buckets),
	}
	actual, _ := v.series.LoadOrStore(key, entry)
	stored, ok := actual.(*seriesEntry[*Histogram])
	if !ok {
		return nil
	}

	return stored.value
}

func (v *HistogramVec) Observe(labels Labels, value float64) {
	hist := v.With(labels)
	if hist != nil {
		hist.Observe(value)
	}
}

func (v *HistogramVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "histogram") {
		return false
	}

	for _, entry := range collectSeries[*Histogram](&v.series) {
		snap := entry.value.Snapshot()
		if len(snap.UpperBounds) != len(snap.Cumulative) {
			return false
		}

		for i, ub := range snap.UpperBounds {
			bucketLabels := appendLabel(entry.labels, labelPair{name: "le", value: strconv.FormatFloat(ub, 'g', -1, 64)})
			if !writeMetricSample(w, v.name+"_bucket", bucketLabels, strconv.FormatUint(snap.Cumulative[i], 10)) {
				return false
			}
		}

		infLabels := appendLabel(entry.labels, labelPair{name: "le", value: "+Inf"})
		if !writeMetricSample(w, v.name+"_bucket", infLabels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}
		if !writeMetricSample(w, v.name+"_sum", entry.labels, strconv.FormatFloat(snap.Sum, 'g', -1, 64)) {
			return false
		}
		if !writeMetricSample(w, v.name+"_count", entry.labels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}
	}

	return true
}

func collectSeries[T any](series *sync.Map) []*seriesEntry[T] {
	entries := make([]*seriesEntry[T], 0)
	series.Range(func(_, value any) bool {
		if entry, ok := value.(*seriesEntry[T]); ok {
			entries = append(entries, entry)
		}

		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	return entries
}

func canonicalLabels(labels Labels) (string, []labelPair) {
	pairs := labelsFromMap(labels)
	var key strings.Builder
	for _, pair := range pairs {
		_, _ = fmt.Fprintf(&key, "%d:%s=%d:%s;", len(pair.name), pair.name, len(pair.value), pair.value)
	}

	return key.String(), pairs
}

func labelsFromMap(labels Labels) []labelPair {
	if len(labels) == 0 {
		return nil
	}

	pairs := make([]labelPair, 0, len(labels))
	for name, value := range labels {
		pairs = append(pairs, labelPair{name: name, value: value})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].name < pairs[j].name
	})

	return pairs
}
