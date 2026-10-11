package jsonutil

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"strings"
	"testing"
)

func TestExtract_NestingLimitMatchesJSONText(t *testing.T) {
	t.Parallel()

	atLimit := strings.Repeat("[", extractMaxDepth) + strings.Repeat("]", extractMaxDepth)
	if !jsontext.Value(atLimit).IsValid() || jsontext.Value("["+atLimit+"]").IsValid() {
		t.Fatal("extractor nesting limit differs from jsontext")
	}

	for _, input := range []string{atLimit, "prefix " + atLimit, "[" + atLimit + "]"} {
		got, err := Extract(input)
		if err != nil || string(got) != atLimit {
			t.Fatalf("Extract() returned %d bytes, %v; want innermost %d bytes", len(got), err, len(atLimit))
		}
	}
}

func TestExtract_LargeBracketFlood(t *testing.T) {
	t.Parallel()

	for _, opener := range []string{"{", "["} {
		if _, err := Extract(strings.Repeat(opener, 200000)); !errors.Is(err, ErrNoJSONFound) {
			t.Fatalf("Extract(%q flood) error = %v, want ErrNoJSONFound", opener, err)
		}
	}
}

func TestExtract_BracketFloodAtInputLimit(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("[", DefaultExtractMaxBytes-1) + "]"
	got, err := Extract(input)

	if err != nil || string(got) != "[]" {
		t.Fatalf("Extract(at-limit bracket flood) = %q, %v; want []", got, err)
	}
}

func FuzzExtractFirstJSON(f *testing.F) {
	for _, input := range []string{
		`{"bad":[1,],"ok":{"v":2}}`,
		`{"text":"[1,2]",bad}`,
		`{"text":"[","]" broken}`,
		`{"a":[],"\u0061":1}`,
		`[{"s":"\\\"{}"}]`,
		`[01,{}]`,
		"[\"\xff\",{}]",
	} {
		f.Add(input)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 512 {
			t.Skip("quadratic reference is only for small differential inputs")
		}

		assertReferenceExtraction(t, input)
	})
}

func assertReferenceExtraction(t *testing.T, input string) {
	t.Helper()

	want, wantErr := referenceFirstJSON(input)
	got, err := extractFirstJSON(input)

	if !errors.Is(err, wantErr) || !bytes.Equal(got, want) {
		t.Fatalf("extractFirstJSON(%q) = %q, %v; reference = %q, %v", input, got, err, want, wantErr)
	}
}

// This oracle searches every raw opener independently, including those in quoted
// prose. Same-type balancing intentionally ignores mismatched other brackets;
// jsontext then rejects malformed candidates, just as the extraction contract does.
func referenceFirstJSON(input string) ([]byte, error) {
	for start := range len(input) {
		open := input[start]
		if open != '{' && open != '[' {
			continue
		}

		closing := byte('}')

		if open == '[' {
			closing = ']'
		}

		depth, quoted, escaped := 0, false, false

		for end := start; end < len(input); end++ {
			c := input[end]

			if quoted {
				switch {
				case escaped:
					escaped = false
				case c == '\\':
					escaped = true
				case c == '"':
					quoted = false
				}

				continue
			}

			switch c {
			case '"':
				quoted = true
			case open:
				depth++
			case closing:
				depth--
			}

			if depth == 0 {
				candidate := []byte(input[start : end+1])
				if jsontext.Value(candidate).IsValid() {
					return candidate, nil
				}

				break
			}
		}
	}

	return nil, ErrNoJSONFound
}
