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
