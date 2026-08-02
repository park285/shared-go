package openaipreset

import (
	"strings"
	"testing"
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

func TestLayeredPromptLenSelectsActiveLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                                 string
		systemPrompt, invariantPrompt, developerPrompt, user string
		want                                                 int
	}{
		{name: "no layers falls back to system", systemPrompt: "sys", user: "usr", want: len("sys\nusr")},
		{name: "blank layers ignored", systemPrompt: "sys", invariantPrompt: "  ", developerPrompt: "\n", user: "usr", want: len("sys\nusr")},
		{name: "invariant only", invariantPrompt: "inv", user: "usr", want: len("inv\nusr")},
		{name: "developer only", developerPrompt: "dev", user: "usr", want: len("dev\nusr")},
		{name: "both layers", invariantPrompt: "inv", developerPrompt: "dev", user: "usr", want: len("inv\ndev\nusr")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := layeredPromptLen(tt.systemPrompt, tt.invariantPrompt, tt.developerPrompt, tt.user)
			if got != tt.want {
				t.Fatalf("layeredPromptLen() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCompletionPromptLenMatchesJoinedTrimmedMessages(t *testing.T) {
	t.Parallel()

	const roleUser = "user"
	messages := []Message{
		{Role: "system", Content: "  sys  "},
		{Role: roleUser, Content: ""},
		{Role: "developer", Content: "\n"},
		{Role: roleUser, Content: " usr "},
	}
	want := len("sys" + "\n" + "usr")
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
