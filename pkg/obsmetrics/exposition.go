package obsmetrics

import (
	"io"
	"strconv"

	"github.com/park285/shared-go/v2/pkg/internal/promtext"
)

// CounterSeries는 counter metric family에 포함될 label-set과 값을 나타냅니다.
type CounterSeries struct {
	Labels Labels
	Value  uint64
}

// WriteCounter는 단일 counter 메트릭을 Prometheus 텍스트 포맷으로 씁니다.
func WriteCounter(w io.Writer, name, help string, value uint64) bool {
	return writeScalar(w, name, help, "counter", strconv.FormatUint(value, 10))
}

// WriteCounterSeries는 family header를 한 번 쓰고 모든 counter series를 씁니다.
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

// WriteGauge는 단일 gauge 메트릭을 씁니다. Value는 호출측이 직렬화한 문자열입니다(정수/실수 모두 수용).
func WriteGauge(w io.Writer, name, help, value string) bool {
	return writeScalar(w, name, help, "gauge", value)
}

func writeScalar(w io.Writer, name, help, metricType, value string) bool {
	if !writeMetricHeader(w, name, help, metricType) {
		return false
	}

	return writeMetricSample(w, name, nil, value)
}

// obsmetrics의 공개 writer는 첫 쓰기 실패에서 멈추고 false를 돌려주는 bool 계약을 가지므로 promtext 오류를 여기서 접는다.
func writeMetricHeader(w io.Writer, name, help, metricType string) bool {
	return promtext.WriteHeader(w, name, help, metricType) == nil
}

func writeMetricSample(w io.Writer, name string, labels []promtext.Label, value string) bool {
	return promtext.WriteSample(w, name, labels, value) == nil
}

// WriteHistogram은 HistogramSnapshot을 Prometheus 텍스트 포맷(bucket/sum/count)으로 씁니다.
// 경계와 누적값 길이가 다른 snapshot이면 아무것도 쓰지 않고 false를 반환합니다.
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
