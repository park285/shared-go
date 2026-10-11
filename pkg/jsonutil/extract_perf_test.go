package jsonutil

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestExtract_HostileInputs(t *testing.T) {
	t.Parallel()

	for _, size := range []int{4096, 8192, 16384, 32768} {
		for _, tc := range hostileExtractCases(size) {
			t.Run(tc.name+"/"+strconv.Itoa(size), func(t *testing.T) {
				t.Parallel()

				got, err := Extract(tc.input)
				if !errors.Is(err, tc.wantError) || string(got) != tc.wantJSON {
					t.Fatalf("Extract() returned %d bytes, %v; want %d bytes, %v", len(got), err, len(tc.wantJSON), tc.wantError)
				}
			})
		}
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

func hostileExtractCases(size int) []extractCase {
	depth := min(size, extractMaxDepth)

	return []extractCase{
		{name: "object_openers_one_closer", input: strings.Repeat("{", size) + "}", wantJSON: "{}"},
		{name: "array_openers_one_closer", input: strings.Repeat("[", size) + "]", wantJSON: "[]"},
		{name: "object_no_closer", input: strings.Repeat("{", size), wantError: ErrNoJSONFound},
		{name: "array_no_closer", input: strings.Repeat("[", size), wantError: ErrNoJSONFound},
		{name: "balanced_invalid_objects", input: strings.Repeat("{", size) + strings.Repeat("}", size), wantJSON: "{}"},
		{name: "balanced_invalid_arrays", input: strings.Repeat("[", size) + "x" + strings.Repeat("]", size), wantError: ErrNoJSONFound},
		{name: "deep_valid_arrays", input: "prefix " + strings.Repeat("[", size) + strings.Repeat("]", size), wantJSON: strings.Repeat("[", depth) + strings.Repeat("]", depth)},
		{name: "mixed_mismatches", input: strings.Repeat("[{", size) + strings.Repeat("]}", size), wantError: ErrNoJSONFound},
		{name: "escaped_quotes", input: strings.Repeat(`{\"`, size) + "}", wantError: ErrNoJSONFound},
		{name: "overlapping_quoted_candidates", input: strings.Repeat(`["`, size) + "x" + strings.Repeat(`"]`, size), wantJSON: `["x"]`},
		{name: "overlapping_escaped_strings", input: strings.Repeat(`["\\`, size) + `"]`, wantJSON: `["\\"]`},
		{name: "quoted_candidates", input: `{"s":"` + strings.Repeat("[", size) + `]",bad}`, wantJSON: "[]"},
		{name: "late_invalid_wide_array", input: "[" + strings.Repeat("0,", size) + "] {}", wantJSON: "{}"},
		{name: "nested_duplicate_names", input: strings.Repeat(`{"x":`, size) + "0" + strings.Repeat(`,"x":0}`, size), wantError: ErrNoJSONFound},
	}
}

func BenchmarkExtractHostileScaling(b *testing.B) {
	for _, size := range []int{4096, 8192, 16384, 32768} {
		for _, tc := range hostileExtractCases(size) {
			b.Run(tc.name+"/"+strconv.Itoa(size), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(tc.input)))

				for b.Loop() {
					got, err := Extract(tc.input)
					if !errors.Is(err, tc.wantError) || len(got) != len(tc.wantJSON) {
						b.Fatalf("Extract() returned %d bytes, %v; want %d bytes, %v", len(got), err, len(tc.wantJSON), tc.wantError)
					}
				}
			})
		}
	}
}
