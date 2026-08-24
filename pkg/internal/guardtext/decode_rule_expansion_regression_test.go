package guardtext

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

func TestDecodeCandidatesWithContextForRulesExpandsNestedTokenInsideOuterBase64(t *testing.T) {
	t.Parallel()

	result := DecodeCandidatesWithContextForRules(
		"system Y0hKdmJYQjA=:",
		func(candidate string) bool { return strings.Contains(candidate, "system prompt:") },
	)
	if !result.Complete() || !slices.Contains(result.Candidates, "system prompt:") {
		t.Fatalf("result = %#v, want complete nested role candidate", result)
	}
}

func TestDecodeCandidatesWithContextForRulesKeepsBenignNestedShortComplete(t *testing.T) {
	t.Parallel()

	inner := base64.StdEncoding.EncodeToString([]byte("ordinary"))
	outer := base64.StdEncoding.EncodeToString([]byte(inner))
	result := DecodeCandidatesWithContextForRules(
		"system "+outer+":",
		func(candidate string) bool { return strings.Contains(candidate, "system prompt:") },
	)

	if !result.Complete() || len(result.Candidates) != 0 {
		t.Fatalf("result = %#v, want complete benign result", result)
	}
}

func TestRootContainsValueRequiresIndependentEncodedToken(t *testing.T) {
	t.Parallel()

	decoder := contextDecoder{roots: []string{"prefix cHJvbXB0 suffix", "Y0hKdmJYQjA="}}
	if !decoder.rootContainsValue("cHJvbXB0") {
		t.Fatal("standalone root token was not found")
	}

	decoder.roots = []string{"Y0hKdmJYQjA="}
	if decoder.rootContainsValue("cHJvbXB0") {
		t.Fatal("nested substring inside outer Base64 was treated as an independent root token")
	}
}
