package obsmetrics

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CounterSeries는 counter metric family에 포함될 label-set과 값을 나타냅니다.
type CounterSeries struct {
	Labels Labels
	Value  uint64
}

// GaugeSeries는 gauge metric family에 포함될 label-set과 직렬화된 정수/실수 값을 나타냅니다.
type GaugeSeries struct {
	Labels Labels
	Value  string
}

// WriteCounter는 단일 counter 메트릭을 Prometheus 텍스트 포맷으로 씁니다.
func WriteCounter(w io.Writer, name, help string, value uint64) bool {
	return WriteCounterWithLabels(w, name, help, nil, value)
}

// WriteCounterWithLabels는 라벨이 있는 counter 메트릭을 Prometheus 텍스트 포맷으로 씁니다.
func WriteCounterWithLabels(w io.Writer, name, help string, labels Labels, value uint64) bool {
	return writeScalar(w, name, help, "counter", labels, strconv.FormatUint(value, 10))
}

// WriteCounterSeries는 family header를 한 번 쓰고 모든 counter series를 씁니다.
func WriteCounterSeries(w io.Writer, name, help string, series []CounterSeries) bool {
	if !writeMetricHeader(w, name, help, "counter") {
		return false
	}
	for _, entry := range series {
		if !writeMetricSample(w, name, labelsFromMap(entry.Labels), strconv.FormatUint(entry.Value, 10)) {
			return false
		}
	}
	return true
}

// WriteGauge는 단일 gauge 메트릭을 씁니다. value는 호출측이 직렬화한 문자열입니다(정수/실수 모두 수용).
func WriteGauge(w io.Writer, name, help, value string) bool {
	return WriteGaugeWithLabels(w, name, help, nil, value)
}

// WriteGaugeWithLabels는 라벨이 있는 gauge 메트릭을 씁니다. value는 호출측이 직렬화한 문자열입니다.
func WriteGaugeWithLabels(w io.Writer, name, help string, labels Labels, value string) bool {
	return writeScalar(w, name, help, "gauge", labels, value)
}

// WriteGaugeSeries는 family header를 한 번 쓰고 모든 gauge series를 씁니다.
func WriteGaugeSeries(w io.Writer, name, help string, series []GaugeSeries) bool {
	if !writeMetricHeader(w, name, help, "gauge") {
		return false
	}
	for _, entry := range series {
		if !writeMetricSample(w, name, labelsFromMap(entry.Labels), entry.Value) {
			return false
		}
	}
	return true
}

func writeScalar(w io.Writer, name, help, metricType string, labels Labels, value string) bool {
	if !writeMetricHeader(w, name, help, metricType) {
		return false
	}

	return writeMetricSample(w, name, labelsFromMap(labels), value)
}

func writeMetricHeader(w io.Writer, name, help, metricType string) bool {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, SanitizeHelp(help)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType); err != nil {
		return false
	}

	return true
}

func writeMetricSample(w io.Writer, name string, labels []labelPair, value string) bool {
	if _, err := fmt.Fprintf(w, "%s%s %s\n", name, formatLabels(labels), value); err != nil {
		return false
	}

	return true
}

// WriteHistogram은 HistogramSnapshot을 Prometheus 텍스트 포맷(bucket/sum/count)으로 씁니다.
func WriteHistogram(w io.Writer, name, help string, snap HistogramSnapshot) bool {
	return WriteHistogramWithLabels(w, name, help, nil, snap)
}

// WriteHistogramWithLabels는 라벨이 있는 HistogramSnapshot을 Prometheus 텍스트 포맷(bucket/sum/count)으로 씁니다.
func WriteHistogramWithLabels(w io.Writer, name, help string, labels Labels, snap HistogramSnapshot) bool {
	return writeHistogram(w, name, help, labelsFromMap(labels), snap)
}

func writeHistogram(w io.Writer, name, help string, labels []labelPair, snap HistogramSnapshot) bool {
	if len(snap.UpperBounds) != len(snap.Cumulative) {
		return false
	}

	if !writeMetricHeader(w, name, help, "histogram") {
		return false
	}

	for i, ub := range snap.UpperBounds {
		bucketLabels := appendLabel(labels, labelPair{name: "le", value: strconv.FormatFloat(ub, 'g', -1, 64)})
		if !writeMetricSample(w, name+"_bucket", bucketLabels, strconv.FormatUint(snap.Cumulative[i], 10)) {
			return false
		}
	}

	infLabels := appendLabel(labels, labelPair{name: "le", value: "+Inf"})
	if !writeMetricSample(w, name+"_bucket", infLabels, strconv.FormatUint(snap.Total, 10)) {
		return false
	}

	if !writeMetricSample(w, name+"_sum", labels, strconv.FormatFloat(snap.Sum, 'g', -1, 64)) {
		return false
	}

	return writeMetricSample(w, name+"_count", labels, strconv.FormatUint(snap.Total, 10))
}

func appendLabel(labels []labelPair, label labelPair) []labelPair {
	out := make([]labelPair, 0, len(labels)+1)
	out = append(out, labels...)
	out = append(out, label)

	return out
}

func formatLabels(labels []labelPair) string {
	if len(labels) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteByte('{')
	for i, label := range labels {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(label.name)
		builder.WriteString("=\"")
		builder.WriteString(escapeLabelValue(label.value))
		builder.WriteByte('"')
	}
	builder.WriteByte('}')

	return builder.String()
}

var labelValueEscaper = strings.NewReplacer(
	`\`, `\\`,
	"\n", `\n`,
	`"`, `\"`,
)

func escapeLabelValue(value string) string {
	return labelValueEscaper.Replace(value)
}

// SanitizeHelp는 HELP 텍스트의 개행을 제거해 exposition 포맷이 깨지지 않게 합니다.
func SanitizeHelp(help string) string {
	return strings.ReplaceAll(help, "\n", " ")
}
