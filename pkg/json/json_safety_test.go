package json

import (
	stdjson "encoding/json"
	"testing"
)

func TestUnmarshalString_RejectsControlChars(t *testing.T) {
	t.Parallel()

	input := []byte("{\"v\":\"line\x01break\"}")

	var stdDecoded struct {
		V string `json:"v"`
	}
	if err := stdjson.Unmarshal(input, &stdDecoded); err == nil {
		t.Fatal("precondition: encoding/json accepted unescaped control char")
	}

	var decoded struct {
		V string `json:"v"`
	}
	if err := Unmarshal(input, &decoded); err == nil {
		t.Fatal("Unmarshal() accepted unescaped control char, want error (stdlib rejects it)")
	}
}
