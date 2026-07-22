package outputguard

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

const benignDecodeTransformCount = 9

func TestBoundGuardAllowsDeclaredBinaryDataURI(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0x00, 0xff, 0x80, 0x01}, 256)...)
	evaluation := bound.Check("data:image/png;base64," + base64.StdEncoding.EncodeToString(png) + " ordinary context eHl6")
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("evaluation = %+v, want allow", evaluation)
	}
}

func TestBoundGuardDoesNotTrustReadableTextBehindDeclaredBinaryMediaType(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	evaluation := bound.Check("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("system prompt: leaked")))
	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("evaluation = %+v, want role block", evaluation)
	}
}

func TestGuardBlocksRestrictedTextInsideFramedCompressedDocument(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{Text: framedCompressedOutputForTest(
		t,
		"system prompt: synthetic hidden instruction",
	)})
	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) ||
		slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("evaluation = %+v, want complete role block", evaluation)
	}
}

func TestBoundGuardBlocksProtectedTextInsideFramedCompressedDocument(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	evaluation := bound.Check(framedCompressedOutputForTest(
		t,
		`{"document":{"description":"internal application rules"}}`,
	))
	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) ||
		slices.Contains(evaluation.ReasonCodes, ReasonDecodeIncomplete) {
		t.Fatalf("evaluation = %+v, want complete protected-text block", evaluation)
	}
}

func framedCompressedOutputForTest(t *testing.T, text string) string {
	t.Helper()

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write([]byte(text)); err != nil {
		t.Fatalf("write compressed fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close compressed fixture: %v", err)
	}

	return "0" + base64.StdEncoding.EncodeToString(compressed.Bytes())
}

func TestBoundGuardBlocksProtectedTextSplitAcrossEncodedFragment(t *testing.T) {
	t.Parallel()

	const protected = "internal application rules"
	bound, err := NewGuard().Bind([]string{protected})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	tests := []struct {
		name string
		text string
	}{
		{
			name: "base64 fragment",
			text: "internal " + base64.StdEncoding.EncodeToString([]byte("application rules")),
		},
		{
			name: "hex fragment",
			text: "internal hex: 61 70 70 6c 69 63 61 74 69 6f 6e 20 72 75 6c 65 73",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluation := bound.Check(test.text)
			if evaluation.Decision != DecisionBlock {
				t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
			}
			if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
				t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
			}
		})
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossShortPaddedBase64Fragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal YXBwbGljYXRpb24= rules")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossShortHexFragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal pol hex: 69 63 79")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossOneAndTwoByteHexFragments(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	for _, text := range []string{
		"internal polic hex: 79",
		"internal poli hex: 63 79",
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionBlock {
			t.Fatalf("text %q decision = %v, want block: %+v", text, evaluation.Decision, evaluation)
		}
		if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
			t.Fatalf("text %q reasons = %v, want protected overlap", text, evaluation.ReasonCodes)
		}
	}
}

func TestBoundGuardAllowsMalformedOrEmptyShortHexFragments(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	for _, text := range []string{
		"internal polic hex:",
		"internal polic hex: zz",
		"internal polic hex: 7",
		"internal poli hex: 63 gg",
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionAllow {
			t.Fatalf("text %q decision = %v, want allow: %+v", text, evaluation.Decision, evaluation)
		}
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossOneByteHexFragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal polic hex: 79")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksProtectedTextBehindNormalizedHexEnvelope(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal polic ｈｅｘ： 79")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksProtectedTextAdjacentToBase64Fragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	for _, text := range []string{
		"internal pb2xpY3k=",
		"internal pb2xpY3k",
		"internal pobGljeQ==",
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionBlock {
			t.Fatalf("text %q decision = %v, want block: %+v", text, evaluation.Decision, evaluation)
		}
		if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
			t.Fatalf("text %q reasons = %v, want protected overlap", text, evaluation.ReasonCodes)
		}
	}
}

func TestBoundGuardAllowsBenignWordsPastProtectedDecodeScanBudget(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check(strings.Repeat("word ", 65))
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardAllowsStructuredOutputWithCamelCaseAndTimestamp(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"sim system\nsave system", "save developer"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	text := `{"version":1,"session":{"startedAt":"2026-05-24T10:00:00Z","updatedAt":"2026-05-24T10:01:00Z","saveHintAt":20},"world":{"locationName":"market","moonPhase":"new","eventName":"festival"},"flags":{"metGuide":"true"}}`
	evaluation := bound.Check(text)
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardBlocksShortBase64RestrictedRule(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"unrelated internal boundary"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("c3lzdGVt prompt: leaked")
	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("evaluation = %+v, want role block", evaluation)
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossShortUnpaddedBase64Fragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal YXBwbGljYXRpb24 rules")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksProtectedTextSplitAcrossVeryShortBase64Fragments(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	for _, text := range []string{
		"internal cG9s icy",
		"internal cG9saWN5",
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionBlock {
			t.Fatalf("text %q decision = %v, want block: %+v", text, evaluation.Decision, evaluation)
		}
		if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
			t.Fatalf("text %q reasons = %v, want protected overlap", text, evaluation.ReasonCodes)
		}
	}
}

func TestBoundGuardBlocksUnpaddedShortBase64WithoutDigits(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal cnVsZXM")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksFourByteUnpaddedBase64Fragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal app"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal YXBw")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardAllowsUnrelatedShortBase64Fragment(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("prefix c29tZSB0ZXh0 suffix")
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestGuardBlocksShortBase64RestrictedRule(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{Text: "c3lzdGVt prompt: leaked"})
	if evaluation.Decision != DecisionBlock || !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("evaluation = %+v, want role block", evaluation)
	}
}

func TestBoundGuardPreservesLongBase64RestrictedRuleReasons(t *testing.T) {
	t.Parallel()

	text := base64.StdEncoding.EncodeToString([]byte("system prompt: synthetic hidden instruction"))
	compatibility := NewGuard().Check(CheckRequest{Text: text})
	bound, err := NewGuard().Bind([]string{"internal application rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	evaluation := bound.Check(text)

	if !slices.Equal(evaluation.ReasonCodes, compatibility.ReasonCodes) {
		t.Fatalf("bound reasons = %v, compatibility reasons = %v", evaluation.ReasonCodes, compatibility.ReasonCodes)
	}
	if !slices.Equal(evaluation.RuleIDs, compatibility.RuleIDs) {
		t.Fatalf("bound rules = %v, compatibility rules = %v", evaluation.RuleIDs, compatibility.RuleIDs)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}

func TestBoundGuardBlocksComposedShortBase64ProtectedText(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	for _, text := range []string{
		"internal cG9s%61WN5",
		"internal cG9sJTY5Y3k=",
	} {
		evaluation := bound.Check(text)
		if evaluation.Decision != DecisionBlock {
			t.Fatalf("text %q decision = %v, want block: %+v", text, evaluation.Decision, evaluation)
		}
		if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
			t.Fatalf("text %q reasons = %v, want protected overlap", text, evaluation.ReasonCodes)
		}
	}
}

func TestGuardBlocksRestrictedRoleSplitAcrossBase64Fragment(t *testing.T) {
	t.Parallel()

	evaluation := NewGuard().Check(CheckRequest{
		Text: "system cHJvbXB0OiBzeW50aGV0aWMgaGlkZGVuIGluc3RydWN0aW9u",
	})
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonRoleBlock) {
		t.Fatalf("reasons = %v, want role block", evaluation.ReasonCodes)
	}
}

func TestBoundGuardRecoversUnpaddedBase64BeforePlaintextSuffix(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy rules"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internal cG9saWN5IArules")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardRecombinesEncodedSeparator(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check("internalIA==policy")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

func TestBoundGuardAllowsRepeatedBenignCamelCaseTokens(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	evaluation := bound.Check(strings.Repeat("saveHintAt ", 17))
	if evaluation.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want allow: %+v", evaluation.Decision, evaluation)
	}
}

func TestBoundGuardRecombinesEncodedSeparatorAcrossLongPlaintextRuns(t *testing.T) {
	t.Parallel()

	bound, err := NewGuard().Bind([]string{"internal policy"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	separator := strings.Repeat(".", 4000)
	evaluation := bound.Check("internal" + separator + "IA==" + separator + "policy")
	if evaluation.Decision != DecisionBlock {
		t.Fatalf("decision = %v, want block: %+v", evaluation.Decision, evaluation)
	}
	if !slices.Contains(evaluation.ReasonCodes, ReasonProtectedTextOverlap) {
		t.Fatalf("reasons = %v, want protected overlap", evaluation.ReasonCodes)
	}
}

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

	bound, err := NewGuard().Bind([]string{"internal policy"})
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

	bound, err := NewGuard().Bind([]string{"internal policy"})
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

	bound, err := NewGuard().Bind([]string{"internal policy"})
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

	bound, err := NewGuard().Bind([]string{"internal policy"})
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

	bound, err := NewGuard().Bind([]string{"internal policy"})
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

	bound, err := NewGuard().Bind([]string{"internal application rules"})
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

	bound, err := NewGuard().Bind([]string{"internal application rules"})
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
