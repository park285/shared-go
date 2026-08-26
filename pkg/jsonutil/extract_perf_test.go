package jsonutil

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtract_BracketFloodTerminates(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("{", 200000)

	done := make(chan error, 1)

	go func() {
		_, err := Extract(input)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNoJSONFound) {
			t.Fatalf("Extract() error = %v, want ErrNoJSONFound", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Extract() did not terminate within 3s on bracket flood")
	}
}

func benchPayload(entries int) string {
	var b strings.Builder

	b.WriteString(`{"items":[`)

	for i := range entries {
		if i > 0 {
			b.WriteByte(',')
		}

		b.WriteString(`{"id":`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`,"name":"member-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","note":"some free text with braces { } and quotes \"q\""}`)
	}

	b.WriteString(`],"ok":true}`)

	return b.String()
}

func BenchmarkExtractWholeDocument(b *testing.B) {
	input := benchPayload(200)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Extract(input); err != nil {
			b.Fatalf("Extract() error = %v", err)
		}
	}
}

func BenchmarkExtractFencedDocument(b *testing.B) {
	input := "Here is the result:\n```json\n" + benchPayload(200) + "\n```\nDone!"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Extract(input); err != nil {
			b.Fatalf("Extract() error = %v", err)
		}
	}
}

func BenchmarkExtractEmbeddedDocument(b *testing.B) {
	input := strings.Repeat("prose ", 4096) + `{"ok":true}` + strings.Repeat(" trailing", 4096)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Extract(input); err != nil {
			b.Fatalf("Extract() error = %v", err)
		}
	}
}

func TestExtract_ArrayBracketFloodTerminates(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("[", 200000)

	done := make(chan error, 1)

	go func() {
		_, err := Extract(input)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNoJSONFound) {
			t.Fatalf("Extract() error = %v, want ErrNoJSONFound", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Extract() did not terminate within 3s on array bracket flood")
	}
}
