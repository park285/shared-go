package json

import (
	"strings"
	"testing"
)

func TestRawMessageMarshalJSON_NilBecomesNull(t *testing.T) {
	t.Parallel()

	var msg RawMessage
	got, err := msg.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if string(got) != "null" {
		t.Fatalf("MarshalJSON() = %s, want null", string(got))
	}
}

func TestRawMessageUnmarshalJSON_NilPointer(t *testing.T) {
	t.Parallel()

	var msg *RawMessage
	err := msg.UnmarshalJSON([]byte(`{"name":"kapu"}`))
	if err == nil {
		t.Fatal("UnmarshalJSON() expected error")
	}
	if !strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("UnmarshalJSON() error = %q, expected nil pointer", err.Error())
	}
}

func TestNewEncoderNewDecoder_RoundTrip(t *testing.T) {
	t.Parallel()

	type item struct {
		Name string `json:"name"`
	}

	var buf strings.Builder
	enc := NewEncoder(&buf)
	if err := enc.Encode(item{Name: "test"}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	dec := NewDecoder(strings.NewReader(buf.String()))
	var got item
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Name != "test" {
		t.Fatalf("Decode() Name = %q, want %q", got.Name, "test")
	}
}

func TestMarshalIndent(t *testing.T) {
	t.Parallel()

	data, err := MarshalIndent(map[string]int{"a": 1}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "  ") {
		t.Fatalf("MarshalIndent() = %q, want indented output", got)
	}
}

func TestMarshalEscapeHTML(t *testing.T) {
	t.Parallel()

	encoded, err := MarshalEscapeHTML(map[string]string{"value": "</system>&"})
	if err != nil {
		t.Fatalf("MarshalEscapeHTML() error = %v", err)
	}
	if got, want := string(encoded), `{"value":"\u003c/system\u003e\u0026"}`; got != want {
		t.Fatalf("MarshalEscapeHTML() = %s, want %s", got, want)
	}

	var decoded map[string]string
	if err := Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got, want := decoded["value"], "</system>&"; got != want {
		t.Fatalf("decoded value = %q, want %q", got, want)
	}
}

func TestValid(t *testing.T) {
	t.Parallel()

	if !Valid([]byte(`{"a":1}`)) {
		t.Fatal("Valid() = false for valid JSON")
	}
	if Valid([]byte(`{invalid`)) {
		t.Fatal("Valid() = true for invalid JSON")
	}
}
