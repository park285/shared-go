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

func TestRenderListContentIndentIsNotIndentedCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, input, want string }{
		{"paragraph", "- 항목\n\n    설명 **굵게**\n\n- 다음", "⦁ 항목\n\n  설명 ❪굵게❫\n⦁ 다음"},
		{"nested paragraph", "- 상위\n\n    - 하위\n\n        하위 **문단**", "⦁ 상위\n\n  ￮ 하위\n\n    하위 ❪문단❫"},
		{"top level code after list", "- 항목\n\n본문\n\n    **코드**", "⦁ 항목\n\n본문\n\n    **코드**"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}

	input := "1. 설치합니다.\n\n    ```bash\n    **pip** install\n    ```\n\n2. 실행합니다."

	ranges := CodeRanges(input)
	if len(ranges) != 1 || !ranges[0].Fenced || ranges[0].Language != "bash" {
		t.Fatalf("CodeRanges() = %+v, want one bash fence", ranges)
	}

	got := Render(input)
	if !strings.Contains(got, "┏━━━━━ bash ━━━━━┓") || !strings.Contains(got, "**pip** install") || strings.Contains(got, "```") {
		t.Fatalf("Render() = %q, want boxed list fence", got)
	}
}
