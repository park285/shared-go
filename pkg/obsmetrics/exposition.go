package obsmetrics

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteCounter는 단일 counter 메트릭을 Prometheus 텍스트 포맷으로 씁니다.
func WriteCounter(w io.Writer, name, help string, value uint64) bool {
	return writeScalar(w, name, help, "counter", strconv.FormatUint(value, 10))
}

// WriteGauge는 단일 gauge 메트릭을 씁니다. value는 호출측이 직렬화한 문자열입니다(정수/실수 모두 수용).
func WriteGauge(w io.Writer, name, help, value string) bool {
	return writeScalar(w, name, help, "gauge", value)
}

func writeScalar(w io.Writer, name, help, metricType, value string) bool {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, SanitizeHelp(help)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "%s %s\n", name, value); err != nil {
		return false
	}

	return true
}

// WriteHistogram은 HistogramSnapshot을 Prometheus 텍스트 포맷(bucket/sum/count)으로 씁니다.
func WriteHistogram(w io.Writer, name, help string, snap HistogramSnapshot) bool {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, SanitizeHelp(help)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "# TYPE %s histogram\n", name); err != nil {
		return false
	}

	for i, ub := range snap.UpperBounds {
		if _, err := fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %s\n", name, strconv.FormatFloat(ub, 'g', -1, 64), strconv.FormatUint(snap.Cumulative[i], 10)); err != nil {
			return false
		}
	}

	if _, err := fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %s\n", name, strconv.FormatUint(snap.Total, 10)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "%s_sum %s\n", name, strconv.FormatFloat(snap.Sum, 'g', -1, 64)); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(w, "%s_count %s\n", name, strconv.FormatUint(snap.Total, 10)); err != nil {
		return false
	}

	return true
}

// SanitizeHelp는 HELP 텍스트의 개행을 제거해 exposition 포맷이 깨지지 않게 합니다.
func SanitizeHelp(help string) string {
	return strings.ReplaceAll(help, "\n", " ")
}
