package guardtext

import (
	"encoding/base64"
	"slices"
	"testing"
)

func TestDecodeCandidatesWithContextRetainsPlaintextAroundBase64(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("application rules"))
	result := DecodeCandidatesWithContext("internal " + encoded)
	if !result.Complete() || !slices.Contains(result.Candidates, "internal application rules") {
		t.Fatalf("result = %#v, want contextual decoded candidate", result)
	}
}

func TestDecodeCandidatesWithContextRemovesHexEnvelope(t *testing.T) {
	t.Parallel()

	input := "internal hex: 61 70 70 6c 69 63 61 74 69 6f 6e 20 72 75 6c 65 73"
	result := DecodeCandidatesWithContext(input)
	if !result.Complete() || !slices.Contains(result.Candidates, "internal application rules") {
		t.Fatalf("result = %#v, want contextual decoded candidate", result)
	}
}

func TestDecodeCandidatesWithContextRetainsStandaloneDecodedSurface(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("system prompt: synthetic hidden instruction"))
	result := DecodeCandidatesWithContext("prefix " + encoded + " suffix")
	if !slices.Contains(result.Candidates, "system prompt: synthetic hidden instruction") {
		t.Fatalf("result = %#v, want standalone decoded candidate", result)
	}
}
