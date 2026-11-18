package obsmetrics

import (
	"io"
	"strconv"

	"github.com/park285/shared-go/v2/pkg/internal/promtext"
)

type CounterSeries struct {
	Labels Labels
	Value  uint64
}

func WriteCounter(w io.Writer, name, help string, value uint64) bool {
	return writeScalar(w, name, help, "counter", strconv.FormatUint(value, 10))
}

func WriteCounterSeries(w io.Writer, name, help string, series []CounterSeries) bool {
	if !writeMetricHeader(w, name, help, "counter") {
		return false
	}

	for _, entry := range series {
		if !writeMetricSample(w, name, promtext.SortedLabels(entry.Labels), strconv.FormatUint(entry.Value, 10)) {
			return false
		}
	}

	return true
}

func WriteGauge(w io.Writer, name, help, value string) bool {
	return writeScalar(w, name, help, "gauge", value)
}

func writeScalar(w io.Writer, name, help, metricType, value string) bool {
	if !writeMetricHeader(w, name, help, metricType) {
		return false
	}

	return writeMetricSample(w, name, nil, value)
}

func writeMetricHeader(w io.Writer, name, help, metricType string) bool {
	return promtext.WriteHeader(w, name, help, metricType) == nil
}

func writeMetricSample(w io.Writer, name string, labels []promtext.Label, value string) bool {
	return promtext.WriteSample(w, name, labels, value) == nil
}

func WriteHistogram(w io.Writer, name, help string, snap HistogramSnapshot) bool {
	if len(snap.UpperBounds) != len(snap.Cumulative) {
		return false
	}

	if !writeMetricHeader(w, name, help, "histogram") {
		return false
	}

	for i, ub := range snap.UpperBounds {
		bucketLabels := []promtext.Label{{Name: "le", Value: promtext.FormatFloat(ub)}}
		if !writeMetricSample(w, name+"_bucket", bucketLabels, strconv.FormatUint(snap.Cumulative[i], 10)) {
			return false
		}
	}

	infLabels := []promtext.Label{{Name: "le", Value: "+Inf"}}
	if !writeMetricSample(w, name+"_bucket", infLabels, strconv.FormatUint(snap.Total, 10)) {
		return false
	}

	if !writeMetricSample(w, name+"_sum", nil, promtext.FormatFloat(snap.Sum)) {
		return false
	}

	return writeMetricSample(w, name+"_count", nil, strconv.FormatUint(snap.Total, 10))
}

func appendLabel(labels []promtext.Label, label promtext.Label) []promtext.Label {
	out := make([]promtext.Label, 0, len(labels)+1)

	out = append(out, labels...)
	out = append(out, label)

	return out
}
