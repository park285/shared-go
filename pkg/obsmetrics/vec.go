package obsmetrics

import (
	"cmp"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/park285/shared-go/v2/pkg/internal/promtext"
)

type Labels map[string]string

type seriesEntry[T any] struct {
	key    string
	labels []promtext.Label
	value  T
}

const (
	DefaultMaxMetricSeries          = 1024
	defaultMaxMetricLabels          = 16
	defaultMaxMetricLabelNameBytes  = 128
	defaultMaxMetricLabelValueBytes = 256
)

const (
	canonicalLabelPairOverhead = 10
	droppedSeriesSuffix        = "_dropped_series_total"
	droppedSeriesHelp          = "Total series dropped because a cardinality or label limit was reached."
)

type VecOptions struct {
	MaxSeries          int
	MaxLabels          int
	MaxLabelNameBytes  int
	MaxLabelValueBytes int
}

type seriesLimiter struct {
	maxSeries          int64
	maxLabels          int
	maxLabelNameBytes  int
	maxLabelValueBytes int
	count              atomic.Int64
	dropped            atomic.Uint64
}

func newSeriesLimiter(options VecOptions) seriesLimiter {
	if options.MaxSeries <= 0 {
		options.MaxSeries = DefaultMaxMetricSeries
	}

	if options.MaxLabels <= 0 {
		options.MaxLabels = defaultMaxMetricLabels
	}

	if options.MaxLabelNameBytes <= 0 {
		options.MaxLabelNameBytes = defaultMaxMetricLabelNameBytes
	}

	if options.MaxLabelValueBytes <= 0 {
		options.MaxLabelValueBytes = defaultMaxMetricLabelValueBytes
	}

	return seriesLimiter{
		maxSeries:          int64(options.MaxSeries),
		maxLabels:          options.MaxLabels,
		maxLabelNameBytes:  options.MaxLabelNameBytes,
		maxLabelValueBytes: options.MaxLabelValueBytes,
	}
}

func (l *seriesLimiter) canonicalize(labels Labels) (string, []promtext.Label, bool) {
	if len(labels) > l.maxLabels {
		l.dropped.Add(1)

		return "", nil, false
	}

	for name, value := range labels {
		if len(name) > l.maxLabelNameBytes || len(value) > l.maxLabelValueBytes {
			l.dropped.Add(1)

			return "", nil, false
		}
	}

	key, pairs := canonicalLabels(labels)

	return key, pairs, true
}

func (l *seriesLimiter) reserve() bool {
	for {
		current := l.count.Load()
		if current >= l.maxSeries {
			l.dropped.Add(1)

			return false
		}

		if l.count.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (l *seriesLimiter) release() {
	l.count.Add(-1)
}

func (l *seriesLimiter) droppedSeries() uint64 {
	return l.dropped.Load()
}

type seriesStore struct {
	series  sync.Map
	limiter seriesLimiter
}

func (s *seriesStore) loadOrCreate[T any](key string, labels []promtext.Label, create func() T) (T, bool) {
	var zero T

	if actual, ok := s.series.Load(key); ok {
		stored, typed := actual.(*seriesEntry[T])
		if !typed {
			return zero, false
		}

		return stored.value, true
	}

	if !s.limiter.reserve() {
		return zero, false
	}

	entry := &seriesEntry[T]{key: key, labels: labels, value: create()}
	actual, loaded := s.series.LoadOrStore(key, entry)

	if loaded {
		s.limiter.release()
	}

	stored, ok := actual.(*seriesEntry[T])
	if !ok {
		if !loaded {
			s.series.Delete(key)
			s.limiter.release()
		}

		return zero, false
	}

	return stored.value, true
}

func (s *seriesStore) collect[T any]() []*seriesEntry[T] {
	entries := make([]*seriesEntry[T], 0)

	s.series.Range(func(_, value any) bool {
		if entry, ok := value.(*seriesEntry[T]); ok {
			entries = append(entries, entry)
		}

		return true
	})
	slices.SortFunc(entries, func(left, right *seriesEntry[T]) int {
		return cmp.Compare(left.key, right.key)
	})

	return entries
}

type CounterVec struct {
	seriesStore

	name string
	help string
}

type counter struct {
	value atomic.Uint64
}

func NewCounterVec(name, help string) *CounterVec {
	return newCounterVecWithOptions(name, help, VecOptions{})
}

func newCounterVecWithOptions(name, help string, options VecOptions) *CounterVec {
	return &CounterVec{name: name, help: help, limiter: newSeriesLimiter(options)}
}

func (v *CounterVec) with(labels Labels) *counter {
	if v == nil {
		return nil
	}

	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}

	series, ok := v.loadOrCreate(key, labelPairs, func() *counter { return &counter{} })
	if !ok {
		return nil
	}

	return series
}

func (v *CounterVec) Inc(labels Labels) {
	v.Add(labels, 1)
}

func (v *CounterVec) Add(labels Labels, delta uint64) {
	series := v.with(labels)
	if series != nil {
		series.Add(delta)
	}
}

func (v *CounterVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}

	if !writeMetricHeader(w, v.name, v.help, "counter") {
		return false
	}

	for _, entry := range v.collect[*counter]() {
		if !writeMetricSample(w, v.name, entry.labels, strconv.FormatUint(entry.value.Value(), 10)) {
			return false
		}
	}

	return writeDroppedSeries(w, v.name, v.limiter.droppedSeries())
}

func (c *counter) Add(delta uint64) {
	if c != nil {
		c.value.Add(delta)
	}
}

func (c *counter) Value() uint64 {
	if c == nil {
		return 0
	}

	return c.value.Load()
}

type GaugeVec struct {
	seriesStore

	name string
	help string
}

type Gauge struct {
	bits atomic.Uint64
}

func NewGaugeVecWithOptions(name, help string, options VecOptions) *GaugeVec {
	return &GaugeVec{name: name, help: help, limiter: newSeriesLimiter(options)}
}

func (v *GaugeVec) With(labels Labels) *Gauge {
	if v == nil {
		return nil
	}

	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}

	gauge, ok := v.loadOrCreate(key, labelPairs, func() *Gauge { return &Gauge{} })
	if !ok {
		return nil
	}

	return gauge
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

	for _, entry := range v.collect[*Gauge]() {
		if !writeMetricSample(w, v.name, entry.labels, promtext.FormatFloat(entry.value.Value())) {
			return false
		}
	}

	return writeDroppedSeries(w, v.name, v.limiter.droppedSeries())
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

type HistogramVec struct {
	seriesStore

	name    string
	help    string
	buckets []float64
}

func NewHistogramVec(name, help string, buckets []float64) *HistogramVec {
	return NewHistogramVecWithOptions(name, help, buckets, VecOptions{})
}

func NewHistogramVecWithOptions(name, help string, buckets []float64, options VecOptions) *HistogramVec {
	bounds := make([]float64, len(buckets))
	copy(bounds, buckets)

	return &HistogramVec{name: name, help: help, buckets: bounds, limiter: newSeriesLimiter(options)}
}

func (v *HistogramVec) With(labels Labels) *Histogram {
	if v == nil {
		return nil
	}

	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}

	histogram, ok := v.loadOrCreate(key, labelPairs, func() *Histogram {
		return NewHistogram(v.buckets)
	})
	if !ok {
		return nil
	}

	return histogram
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

	for _, entry := range v.collect[*Histogram]() {
		snap := entry.value.Snapshot()
		if len(snap.UpperBounds) != len(snap.Cumulative) {
			return false
		}

		for i, ub := range snap.UpperBounds {
			bucketLabels := appendLabel(entry.labels, promtext.Label{Name: "le", Value: promtext.FormatFloat(ub)})
			if !writeMetricSample(w, v.name+"_bucket", bucketLabels, strconv.FormatUint(snap.Cumulative[i], 10)) {
				return false
			}
		}

		infLabels := appendLabel(entry.labels, promtext.Label{Name: "le", Value: "+Inf"})
		if !writeMetricSample(w, v.name+"_bucket", infLabels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}

		if !writeMetricSample(w, v.name+"_sum", entry.labels, promtext.FormatFloat(snap.Sum)) {
			return false
		}

		if !writeMetricSample(w, v.name+"_count", entry.labels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}
	}

	return writeDroppedSeries(w, v.name, v.limiter.droppedSeries())
}

func writeDroppedSeries(w io.Writer, name string, dropped uint64) bool {
	droppedName := name + droppedSeriesSuffix
	if !writeMetricHeader(w, droppedName, droppedSeriesHelp, "counter") {
		return false
	}

	return writeMetricSample(w, droppedName, nil, strconv.FormatUint(dropped, 10))
}

func canonicalLabels(labels Labels) (string, []promtext.Label) {
	pairs := promtext.SortedLabels(labels)
	if len(pairs) == 0 {
		return "", nil
	}

	size := 0

	for _, pair := range pairs {
		size += len(pair.Name) + len(pair.Value) + canonicalLabelPairOverhead
	}

	var key strings.Builder

	key.Grow(size)

	var lenBuf [20]byte

	for _, pair := range pairs {
		key.Write(strconv.AppendInt(lenBuf[:0], int64(len(pair.Name)), 10))
		key.WriteByte(':')
		key.WriteString(pair.Name)
		key.WriteByte('=')
		key.Write(strconv.AppendInt(lenBuf[:0], int64(len(pair.Value)), 10))
		key.WriteByte(':')
		key.WriteString(pair.Value)
		key.WriteByte(';')
	}

	return key.String(), pairs
}
