package kakaoformat

import (
	"strings"
	"testing"
)

func TestRenderPreservesCodeBodiesAcrossForms(t *testing.T) {
	t.Parallel()

	body := "**literal**\n[x](https://example.com)"
	for _, input := range []string{
		"~~~tex\n" + body + "\n~~~", "```tex\n" + body, "```tex\n" + body + "\n```",
		"    **literal**\n    [x](https://example.com)", "`" + body + "`",
		"``a` **literal** [x](https://example.com)``", "> ~~~tex\n> **literal**\n> [x](https://example.com)\n> ~~~",
	} {
		got := Render(input)
		if !strings.Contains(got, "**literal**") || !strings.Contains(got, "[x](https://example.com)") {
			t.Errorf("input=%q got=%q", input, got)
		}
	}
}

func TestEscapedTextRoundTrip(t *testing.T) {
	t.Parallel()

	for _, literal := range []string{"[x](y)", "**hello**", "# title", "- item", "1. item", "a|b", "~~literal~~", `https://example.com/a_b`, `C:\folder\file`, "> quoted"} {
		if got := Render(EscapeMarkdown(literal)); got != literal {
			t.Errorf("input=%q got=%q", literal, got)
		}
	}

	input := "| expression | description |\n| --- | --- |\n| " + EscapeMarkdown("|x|") + " | preserved |"
	if got := Render(input); !strings.Contains(got, "|x|") || !strings.Contains(got, "preserved") {
		t.Fatalf("table lost content: %q", got)
	}
}

func TestCodeContainerEndDoesNotHideFollowingText(t *testing.T) {
	t.Parallel()

	input := "> ~~~tex\n> **literal**\n\n**outside**"
	got := Render(input)

	if !strings.Contains(got, "**literal**") || strings.Contains(got, "**outside**") {
		t.Fatalf("container boundary: %q", got)
	}
}
