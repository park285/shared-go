package promtext

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

type Label struct {
	Name  string
	Value string
}

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

func FormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func WriteHeader(w io.Writer, name, help, metricType string) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, escapeHelp(help), name, metricType); err != nil {
		return fmt.Errorf("promtext: write header: %w", err)
	}

	return nil
}

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
