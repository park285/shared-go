package promtext

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteHeaderEscapesHelpPerSpec(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	if err := WriteHeader(&buf, "requests_total", "line1\nline2 C:\\path", "counter"); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	want := "# HELP requests_total line1\\nline2 C:\\\\path\n# TYPE requests_total counter\n"
	if got := buf.String(); got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestWriteSampleSortsAndEscapesLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	labels := SortedLabels(map[string]string{"status": "ok", "path": "a\\b\"c\nd"})
	if err := WriteSample(&buf, "requests_total", labels, FormatFloat(12)); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	want := `requests_total{path="a\\b\"c\nd",status="ok"} 12` + "\n"
	if got := buf.String(); got != want {
		t.Fatalf("sample = %q, want %q", got, want)
	}

	buf.Reset()

	if err := WriteSample(&buf, "up", SortedLabels(nil), FormatFloat(math.Inf(1))); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	if got := buf.String(); got != "up +Inf\n" {
		t.Fatalf("unlabeled sample = %q, want %q", got, "up +Inf\n")
	}
}

func TestWriteReturnsWriterError(t *testing.T) {
	t.Parallel()

	if err := WriteHeader(failingWriter{}, "up", "help", "gauge"); err == nil {
		t.Fatal("WriteHeader() error = nil, want writer error")
	}

	if err := WriteSample(failingWriter{}, "up", nil, "1"); err == nil {
		t.Fatal("WriteSample() error = nil, want writer error")
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
