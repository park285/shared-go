package json

import "testing"

func TestRawMessageRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		Data *RawMessage `json:"data,omitempty"`
	}

	var decoded payload
	if err := Unmarshal([]byte(`{"data":[{"id":1}]}`), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.Data == nil {
		t.Fatal("decoded.Data is nil")
	}
	if got, want := string(*decoded.Data), `[{"id":1}]`; got != want {
		t.Fatalf("decoded.Data = %s, want %s", got, want)
	}

	encoded, err := Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(encoded), `{"data":[{"id":1}]}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestNumberParsers(t *testing.T) {
	t.Parallel()

	n := Number("42")
	if got := n.String(); got != "42" {
		t.Fatalf("String() = %s, want 42", got)
	}
	if got, err := n.Int64(); err != nil || got != 42 {
		t.Fatalf("Int64() = (%d, %v), want (42, nil)", got, err)
	}

	f := Number("3.14")
	if got, err := f.Float64(); err != nil || got != 3.14 {
		t.Fatalf("Float64() = (%f, %v), want (3.14, nil)", got, err)
	}
}
