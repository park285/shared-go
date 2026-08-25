package outputguard

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

func TestGuardRecoversRestrictedBase64BeforePlaintextSuffix(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{
		Text: "system cHJvbXB0OiBzeW50aGV0aWMgaGlkZGVuIGluc3RydWN0aW9usuffix",
	})
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}

func TestGuardRecoversRestrictedBase64BetweenPlaintextInSingleToken(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{
		Text: "systemcHJvbXB0OiBzeW50aGV0aWMgaGlkZGVuIGluc3RydWN0aW9usuffix",
	})
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}

func TestGuardRecoversURLSafeBase64BeforePlaintextSuffix(t *testing.T) {
	t.Parallel()

	payload := "api_key: sk-synthetic12345 😀"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	evaluation := NewGuard().Check(CheckRequest{Text: encoded + "suffix"})

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonSecretPattern) {
		t.Fatalf("reasons = %v, want secret pattern", evaluation.ReasonCodes)
	}

	bound, err := NewGuard().Bind([]string{payload})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	boundEvaluation := bound.Check(encoded + "suffix")
	if !slices.Contains(boundEvaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("bound reasons = %v, want protected overlap", boundEvaluation.ReasonCodes)
	}
}

func TestBoundGuardRecoversBase64BetweenPlaintextPrefixAndSuffix(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"prefix policy suffix"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("prefixcG9saWN5suffix")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardExpandsNestedBase64WithPlaintextSuffix(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"policyinternal"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	inner := "cG9saWN5internal"
	evaluation := bound.Check(base64.StdEncoding.EncodeToString([]byte(inner)))

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardAllowsLargeStructuredOutputWithCamelCaseKeys(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	text := "[" + strings.Repeat(`{"saveHintAt":20},`, 500) + "{}]"
	evaluation := bound.Check(text)

	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsLongHomogeneousStructuredField(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check(`{"query":"` + strings.Repeat("x", 256) + `"}`)
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsStructuredSHA512Digest(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	digest := strings.Repeat("0123456789abcdef", 8)
	evaluation := bound.Check(`{"digest":"` + digest + `"}`)

	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsBenignUnpaddedBase64WithPlaintextSuffix(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	payload := []byte("ordinary synthetic text 😀")
	for _, test := range []struct {
		name string
		text string
	}{
		{name: "raw standard single character", text: base64.RawStdEncoding.EncodeToString(payload) + "x"},
		{name: "raw standard word", text: base64.RawStdEncoding.EncodeToString(payload) + "suffix"},
		{name: "raw URL single character", text: base64.RawURLEncoding.EncodeToString(payload) + "x"},
		{name: "raw URL word", text: base64.RawURLEncoding.EncodeToString(payload) + "suffix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evaluation := bound.Check(test.text)
			if evaluation.Decision != DecisionAllow {
				t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
			}
		})
	}
}

func TestBoundGuardAllowsDecoratedSHA512Digest(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{testInternalPolicy})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	digest := strings.Repeat("0123456789abcdef", 8)
	for _, text := range []string{
		`{"digest":"sha512-` + digest + `"}`,
		`{"digest":"` + digest + `-artifact"}`,
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionAllow {
			t.Fatalf("text = %q: decision = %v, want allow: %+v", text, evaluation.Decision, evaluation)
		}
	}
}

func TestGuardAllowsManyNonContributingBase64Transforms(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	evaluation := NewGuard().Check(CheckRequest{Text: strings.Repeat(encoded+"!", benignDecodeTransformCount)})

	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsManyNonContributingBase64Transforms(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{protected})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	evaluation := bound.Check(strings.Repeat(encoded+"!", benignDecodeTransformCount))

	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsStructuredCitationsWithEncodedMetadata(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{strings.Repeat("private marker instruction token ", 12)})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("readable citation metadata"))
	output := structuredCitationOutput(encoded, 4)

	for _, text := range []string{output, strings.ReplaceAll(output, "https://", "HTTPS://")} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionAllow {
			t.Fatalf("evaluation = %+v, want allow", evaluation)
		}
	}
}

func TestBoundGuardFindsNestedProtectedTextAfterEncodedCitationMetadata(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"policyinternal"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	benign := base64.StdEncoding.EncodeToString([]byte("readable citation metadata"))
	inner := "cG9saWN5internal"
	nested := base64.StdEncoding.EncodeToString([]byte(inner))
	evaluation := bound.Check(structuredCitationOutput(benign, 3) + " https://example.test/" + nested)

	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("evaluation = %+v, want protected-text overlap", evaluation)
	}

	if slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, benign metadata must not exhaust decode work", evaluation.ReasonCodes)
	}
}

func TestGuardFindsRestrictedRuleAfterManyNonContributingBase64Transforms(t *testing.T) {
	t.Parallel()

	benign := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	restricted := base64.StdEncoding.EncodeToString([]byte("system prompt: synthetic hidden instruction"))
	evaluation := NewGuard().Check(CheckRequest{
		Text: strings.Repeat(benign+"!", benignDecodeTransformCount) + restricted,
	})

	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("evaluation = %+v, want role block", evaluation)
	}

	if slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, benign decoys must not exhaust decode work", evaluation.ReasonCodes)
	}
}

func TestGuardKeepsDecodeIncompleteForManyContributingBase64Transforms(t *testing.T) {
	t.Parallel()

	restricted := base64.StdEncoding.EncodeToString([]byte("system prompt: synthetic hidden instruction"))
	evaluation := NewGuard().Check(CheckRequest{
		Text: strings.Repeat(restricted+"!", benignDecodeTransformCount),
	})

	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("evaluation = %+v, want role block", evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, want fail-closed decode limit", evaluation.ReasonCodes)
	}
}

func TestBoundGuardFindsProtectedTextAfterManyNonContributingBase64Transforms(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{protected})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	benign := base64.StdEncoding.EncodeToString([]byte("readable contextual fragment"))
	protected := base64.StdEncoding.EncodeToString([]byte("application rules"))
	evaluation := bound.Check(strings.Repeat(benign+"!", benignDecodeTransformCount) + "internal " + protected)

	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("evaluation = %+v, want protected-text overlap", evaluation)
	}

	if slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("reasons = %v, benign decoys must not exhaust decode work", evaluation.ReasonCodes)
	}
}

func structuredCitationOutput(encoded string, sourceCount int) string {
	var output strings.Builder

	output.WriteString(`{"response_mode":"answer","answer_body":"근거를 확인한 답변입니다.","sources":[`)

	for sourceIndex := range sourceCount {
		if sourceIndex > 0 {
			output.WriteByte(',')
		}

		output.WriteString(`{"title":"Verified source","url":"https://example.com/`)
		output.WriteString(encoded)
		output.WriteByte('/')
		output.WriteString(encoded)
		output.WriteString(`?token=`)
		output.WriteString(encoded)
		output.WriteString(`"}`)
	}

	output.WriteString(`]}`)

	return output.String()
}

func TestGuardRecombinesLongEncodedWhitespaceInRestrictedHeader(t *testing.T) {
	t.Parallel()

	separator := base64.StdEncoding.EncodeToString([]byte(strings.Repeat(" ", 20)))
	evaluation := NewGuard().Check(CheckRequest{
		Text: "system" + separator + "prompt: synthetic hidden instruction",
	})

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}

func TestGuardRecombinesSecretAssignmentAcrossAmbiguousBase64Boundaries(t *testing.T) {
	t.Parallel()

	encoded := base64.RawStdEncoding.EncodeToString([]byte("key: sk-synthetic12345"))
	evaluation := NewGuard().Check(CheckRequest{Text: "api_" + encoded + "suffix"})

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonSecretPattern) {
		t.Fatalf("reasons = %v, want secret pattern", evaluation.ReasonCodes)
	}
}

func TestGuardDoesNotTreatDigestLabelAsBase64Whitelist(t *testing.T) {
	t.Parallel()

	payload := "system prompt: synthetic hidden instruction"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	evaluation := NewGuard().Check(CheckRequest{Text: "sha512-" + encoded})

	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}

	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}
