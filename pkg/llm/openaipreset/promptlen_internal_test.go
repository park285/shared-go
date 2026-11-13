package openaipreset

import (
	"strings"
	"testing"

	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

func TestJoinedPromptLenMatchesTrimmedJoin(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"", ""},
		{"a", "b"},
		{"", "b"},
		{"a", ""},
		{"", ""},
		{"  ", "\t\n"},
		{"  leading", "trailing  "},
		{"\n\n", "\n\n"},
		{"한글", "프롬프트"},
		{" nbsp", "tail "},
		{"", "", ""},
		{"invariant", "", "tail"},
		{"", "middle", ""},
		{"  x  ", "  y  ", "  z  "},
		{"head", "\n \t ", "tail"},
	}

	for _, parts := range cases {
		want := len(strings.TrimSpace(strings.Join(parts, "\n")))
		if got := joinedPromptLen(parts...); got != want {
			t.Fatalf("joinedPromptLen(%q) = %d, want %d", parts, got, want)
		}
	}
}

func TestCompletionPromptLenMatchesJoinedTrimmedMessages(t *testing.T) {
	t.Parallel()

	const roleUser = "user"

	messages := []sharedllm.Message{
		{Role: "system", Content: "  sys  "},
		{Role: roleUser, Content: ""},
		{Role: "developer", Content: "\n"},
		{Role: roleUser, Content: " usr "},
	}
	want := len("sys" + "\n" + testShortRoleUser)

	if got := completionPromptLen(messages); got != want {
		t.Fatalf("completionPromptLen() = %d, want %d", got, want)
	}

	if got := completionPromptLen(nil); got != 0 {
		t.Fatalf("completionPromptLen(nil) = %d, want 0", got)
	}
}

func TestJoinedPromptLenAllocatesNothing(t *testing.T) {
	parts := []string{"  invariant layer  ", "developer layer", "user layer  "}

	if allocs := testing.AllocsPerRun(50, func() { _ = joinedPromptLen(parts...) }); allocs != 0 {
		t.Fatalf("joinedPromptLen allocations = %.0f, want 0", allocs)
	}
}
