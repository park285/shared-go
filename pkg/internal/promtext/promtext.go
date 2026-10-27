// Package promtext는 Prometheus text exposition의 HELP·label·값 표기 규칙을 shared-go 안에서 한 곳에 둔다.
// obsmetrics와 workercontract가 같은 규칙으로 쓰도록 두 패키지 모두 이 패키지만 호출한다.
package promtext

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Label은 sample에 붙는 label 한 쌍이다.
type Label struct {
	Name  string
	Value string
}

// SortedLabels는 label map을 name 오름차순 슬라이스로 바꾼다. 빈 map은 nil을 돌려준다.
func SortedLabels(labels map[string]string) []Label {
	if len(labels) == 0 {
		return nil
	}

	pairs := make([]Label, 0, len(labels))
	for name, value := range labels {
		pairs = append(pairs, Label{Name: name, Value: value})
	}

	slices.SortFunc(pairs, func(left, right Label) int {
		return cmp.Compare(left.Name, right.Name)
	})

	return pairs
}

// FormatFloat은 sample 값과 bucket 경계를 가장 짧은 왕복 표기로 쓴다. ±Inf와 NaN은 `+Inf`·`-Inf`·`NaN`이다.
func FormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// WriteHeader는 family의 `# HELP`와 `# TYPE` 줄을 쓴다. HELP의 `\`와 개행은 규격대로 `\\`·`\n`으로 escape한다.
func WriteHeader(w io.Writer, name, help, metricType string) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, escapeHelp(help), name, metricType); err != nil {
		return fmt.Errorf("promtext: write header: %w", err)
	}

	return nil
}

// WriteSample은 `name{labels} value` 한 줄을 쓴다. Label은 호출자가 정한 순서대로 쓰며 값은 escape한다.
func WriteSample(w io.Writer, name string, labels []Label, value string) error {
	if _, err := fmt.Fprintf(w, "%s%s %s\n", name, formatLabels(labels), value); err != nil {
		return fmt.Errorf("promtext: write sample: %w", err)
	}

	return nil
}

func formatLabels(labels []Label) string {
	if len(labels) == 0 {
		return ""
	}

	var builder strings.Builder

	builder.WriteByte('{')

	for i, label := range labels {
		if i > 0 {
			builder.WriteByte(',')
		}

		builder.WriteString(label.Name)
		builder.WriteString("=\"")
		builder.WriteString(escapeLabelValue(label.Value))
		builder.WriteByte('"')
	}

	builder.WriteByte('}')

	return builder.String()
}

var (
	labelValueEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	helpEscaper       = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)

func escapeLabelValue(value string) string {
	return labelValueEscaper.Replace(value)
}

func escapeHelp(help string) string {
	return helpEscaper.Replace(help)
}
