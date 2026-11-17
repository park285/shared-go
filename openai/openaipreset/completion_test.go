package openaipreset_test

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/park285/shared-go/openai/openaipreset"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

func TestCompletionRole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		role string
		want responses.EasyInputMessageRole
	}{
		{testDeveloper, responses.EasyInputMessageRoleDeveloper},
		{" Assistant ", responses.EasyInputMessageRoleAssistant},
		{testSystem, responses.EasyInputMessageRoleSystem},
		{testUser, responses.EasyInputMessageRoleUser},
		{"usr", responses.EasyInputMessageRoleUser},
		{testBlankLayer, responses.EasyInputMessageRoleUser},
	}

	for _, tc := range cases {
		if got := openaipreset.CompletionRole(tc.role); got != tc.want {
			t.Fatalf("CompletionRole(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestCompletionFromResponseUsageAndModel(t *testing.T) {
	t.Parallel()

	var resp responses.Response

	raw := `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"","output":[],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":7},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":17}}`
	if err := jsonv2.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	got := openaipreset.CompletionFromResponse(&resp, " fallback ")
	if got.Model != "fallback" {
		t.Fatalf("Model = %q, want requested model fallback", got.Model)
	}

	want := sharedllm.Usage{InputTokens: 12, OutputTokens: 5, TotalTokens: 17, CachedInputTokens: 2, CacheWriteTokens: 7, ReasoningOutputTokens: 1}
	if got.Usage != want {
		t.Fatalf("Usage = %+v, want %+v", got.Usage, want)
	}

	resp.Model = " gpt-returned "
	if returned := openaipreset.CompletionFromResponse(&resp, "fallback"); returned.Model != "gpt-returned" {
		t.Fatalf("Model = %q, want response model", returned.Model)
	}

	if nilResp := openaipreset.CompletionFromResponse(nil, " requested "); nilResp != (openaipreset.CompletionResponse{Model: "requested"}) {
		t.Fatalf("CompletionFromResponse(nil) = %+v", nilResp)
	}
}

func TestUsageFromResponseUsageNil(t *testing.T) {
	t.Parallel()

	if got := openaipreset.UsageFromResponseUsage(nil); got != (sharedllm.Usage{}) {
		t.Fatalf("UsageFromResponseUsage(nil) = %+v, want zero usage", got)
	}
}

func TestCompletionFromResponseTextSelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "prefers final answer",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\"tool_calls\":[]}","annotations":[]}]},{"id":"msg-1","type":"message","status":"completed","phase":"final_answer","role":"assistant","content":[{"type":"output_text","text":" final ","annotations":[]}]}]}`,
			want: "final",
		},
		{
			name: "skips tool envelope without final",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\"function_call\":{}}","annotations":[]}]}]}`,
			want: "",
		},
		{
			name: "skips formatted tool envelope with reordered key",
			raw:  `{"id":"resp-1","object":"response","created_at":1,"status":"completed","model":"gpt-test","output":[{"id":"msg-0","type":"message","status":"completed","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"{\n  \"metadata\": {},\n  \"tool_calls\": []\n}","annotations":[]}]}]}`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var resp responses.Response

			if err := jsonv2.Unmarshal([]byte(tc.raw), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			got := openaipreset.CompletionFromResponse(&resp, "fallback")
			if got.Text != tc.want {
				t.Fatalf("Text = %q, want %q", got.Text, tc.want)
			}
		})
	}
}
