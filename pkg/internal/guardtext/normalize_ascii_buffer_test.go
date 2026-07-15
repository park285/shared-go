package guardtext

import "testing"

func TestNormalizeASCIIIntoMatchesNormalize(t *testing.T) {
	t.Parallel()

	for first := range 128 {
		for second := range 128 {
			input := []byte{byte(first), byte(second)}
			storage := make([]byte, 0, 16)
			got, ok := NormalizeASCIIInto(storage, input)
			if !ok {
				t.Fatalf("NormalizeASCIIInto(%q) reported non-ASCII", input)
			}
			if want := Normalize(string(input)); string(got) != want {
				t.Fatalf("NormalizeASCIIInto(%q) = %q, want %q", input, got, want)
			}
		}
	}
}

func TestNormalizeASCIIIntoRejectsNonASCII(t *testing.T) {
	t.Parallel()

	if _, ok := NormalizeASCIIInto(nil, []byte("ＳＹＳＴＥＭ")); ok {
		t.Fatal("NormalizeASCIIInto() accepted non-ASCII input")
	}
}
