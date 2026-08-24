package guardtext

import (
	"encoding/base64"
	"slices"
	"strings"
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

func TestDecodeCandidatesWithContextFindsEmbeddedBase64WithoutDelimiter(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("application rules"))
	result := DecodeCandidatesWithContext("internal" + encoded)

	if !result.Complete() || !slices.Contains(result.Candidates, "internalapplication rules") {
		t.Fatalf("result = %#v, want embedded contextual candidate", result)
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

func TestDecodeCandidatesWithContextForProtectedExpandsShortBase64(t *testing.T) {
	t.Parallel()

	result := DecodeCandidatesWithContextForProtected("internal YXBwbGljYXRpb24= rules", nil, nil)
	if !slices.Contains(result.Candidates, "internal application rules") {
		t.Fatalf("result = %#v", result)
	}
}

func TestDecodeCandidatesWithContextMarksOversizeContextualCandidateIncomplete(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	result := DecodeCandidatesWithContext(strings.Repeat("!", maxDecodedCandidateLen+1) + encoded)

	if result.Status&DecodeByteLimit == 0 {
		t.Fatalf("result = %#v, want byte limit", result)
	}

	if !slices.Contains(result.Candidates, "readable contextual fragment") {
		t.Fatalf("result = %#v, want standalone decoded candidate", result)
	}
}

func TestFilteredContextDecodersCompleteBenignBoundaryLikeInputs(t *testing.T) {
	t.Parallel()

	payload := []byte("ordinary synthetic text 😀")
	digest := strings.Repeat("0123456789abcdef", 8)

	for _, input := range []string{
		base64.RawStdEncoding.EncodeToString(payload) + "x",
		base64.RawURLEncoding.EncodeToString(payload) + "suffix",
		`{"digest":"sha512-` + digest + `"}`,
		`{"digest":"` + digest + `-artifact"}`,
	} {
		rules := DecodeCandidatesWithContextForRules(input, func(string) bool { return false })
		if !rules.Complete() {
			t.Errorf("rules input %q: result = %#v, want complete", input, rules)
		}

		protected := DecodeCandidatesWithContextForProtected(input, func(string) bool { return false }, nil)
		if !protected.Complete() {
			t.Errorf("protected input %q: result = %#v, want complete", input, protected)
		}
	}
}

func TestProtectedContextSkipsRepeatedNonContributingBase64Values(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("readable citation metadata"))
	result := DecodeCandidatesWithContextForProtected(
		strings.Repeat(encoded+" ", maxDecodeCandidates+4),
		func(string) bool { return false },
		nil,
	)

	if !result.Complete() || len(result.Candidates) != 0 {
		t.Fatalf("result = %#v, want complete empty result", result)
	}
}

func TestProtectedContextRetainsNestedAmbiguousBase64Boundary(t *testing.T) {
	t.Parallel()

	inner := "cG9saWN5internal"
	result := DecodeCandidatesWithContextForProtected(
		base64.StdEncoding.EncodeToString([]byte(inner)),
		func(candidate string) bool { return strings.Contains(candidate, "policyinternal") },
		nil,
	)

	if !result.Complete() || !slices.Contains(result.Candidates, inner) {
		t.Fatalf("result = %#v, want nested protected candidate", result)
	}
}

func TestProtectedContextRejectsOversizeBeforeMatcherObservation(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("!", maxDecodedCandidateLen+1) + "IA=="
	maxObserved := 0
	result := DecodeCandidatesWithContextForProtected(input, func(candidate string) bool {
		maxObserved = max(maxObserved, len(candidate))

		return true
	}, nil)

	if result.Status&DecodeByteLimit == 0 {
		t.Fatalf("result = %#v, want byte limit", result)
	}

	if maxObserved > maxDecodedCandidateLen {
		t.Fatalf("matcher observed %d bytes, want at most %d", maxObserved, maxDecodedCandidateLen)
	}
}
