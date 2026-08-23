package openaipreset_test

import (
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

func TestLooksLikeToolCallEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{name: "plain prose", text: "hello world", want: false},
		{name: "non object json", text: `["tool_calls"]`, want: false},
		{name: "tool_calls at head", text: `{"tool_calls":[]}`, want: true},
		{name: "function_call", text: `{"function_call":{}}`, want: true},
		{name: "tool_call singular", text: `{"tool_call":{"name":"x"}}`, want: true},
		{name: "key after nested object", text: "{\n  \"metadata\": {},\n  \"tool_calls\": []\n}", want: true},
		{name: "key after nested array of objects", text: `{"a":[{"b":[1,2]},{"c":{}}],"tool_calls":[]}`, want: true},
		{name: "key nested one level deep is not top level", text: `{"payload":{"tool_calls":[]}}`, want: false},
		{name: "value containing the key name", text: `{"text":"tool_calls are fun"}`, want: false},
		{name: "leading whitespace tolerated", text: "  \n {\"tool_calls\":[]}", want: true},
		{name: "malformed before any key", text: `{,}`, want: false},
		{name: "ordinary json object", text: `{"answer":"42","score":1.5,"ok":true,"none":null}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := openaipreset.LooksLikeToolCallEnvelope(tt.text); got != tt.want {
				t.Fatalf("LooksLikeToolCallEnvelope(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

// 전체 파싱본과 달리 조기 종료 판정은 잘린 envelope도 tool-call로 본다.
func TestLooksLikeToolCallEnvelopeTruncatedTailStillDetected(t *testing.T) {
	t.Parallel()

	if !openaipreset.LooksLikeToolCallEnvelope(`{"tool_calls":[{"name":"search"`) {
		t.Fatal("truncated tool-call envelope should still be suppressed")
	}
	if openaipreset.LooksLikeToolCallEnvelope(`{"answer":"partial`) {
		t.Fatal("truncated ordinary object must not be treated as a tool-call envelope")
	}
}

func TestLooksLikeToolCallEnvelopeDoesNotScanWholePayload(t *testing.T) {
	t.Parallel()

	text := `{"tool_calls":[],"filler":"` + strings.Repeat("x", 1<<20) + `"}`
	if !openaipreset.LooksLikeToolCallEnvelope(text) {
		t.Fatal("tool_calls at head should be detected without consuming the tail")
	}
}
