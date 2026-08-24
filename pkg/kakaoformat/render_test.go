package kakaoformat

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRenderConvertsPlainMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "basic",
			in:   "# 제목\n- **강조** 항목\n- `code`\n[링크](https://example.com)",
			want: "【제목】\n\n" +
				"⦁ ❪강조❫ 항목\n" +
				"⦁ ⦗ code ⦘\n" +
				"링크( https://example.com )",
		},
		{
			name: "table",
			in:   "| Name | Score |\n| --- | --- |\n| Kim | 10 |\n| Lee | 20 |",
			want: "【Name】\n" +
				"    《1》 Kim\n" +
				"    《2》 Lee\n" +
				"-------------------------\n" +
				"【Score】\n" +
				"    《1》 10\n" +
				"    《2》 20\n" +
				"-------------------------",
		},
		{
			name: "rule and checkbox",
			in:   "---\n- [x] done\n- [ ] todo",
			want: "━━━━━━━━━━━━━━━━━━━━\n✔ done\n✖ todo",
		},
		{
			name: "image",
			in:   "![도표](https://example.com/a.png)",
			want: "도표( https://example.com/a.png )",
		},
		{
			name: "ascii emphasis",
			in:   "***abc*** **abc** *abc* ~~abc~~",
			want: "𝙖𝙗𝙘 𝗮𝗯𝗰 𝘢𝘣𝘤 a̶b̶c̶",
		},
		{
			name: "hololive alarm",
			in:   "## 🔴 **비비** 방송 시작\n[제목](https://short.holoshi.com/l/abc)",
			want: "【🔴 ❪비비❫ 방송 시작】\n\n제목( https://short.holoshi.com/l/abc )",
		},
		{
			name: "mixed hangul and ascii bold",
			in:   "**비비** live **abc123**",
			want: "❪비비❫ live 𝗮𝗯𝗰𝟭𝟮𝟯",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Render(tt.in)
			if got != tt.want {
				t.Fatalf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderLeavesBlankInput(t *testing.T) {
	t.Parallel()

	if got := Render(""); got != "" {
		t.Fatalf("Render(empty) = %q", got)
	}

	if got := Render("   \n"); got != "   \n" {
		t.Fatalf("Render(whitespace) = %q", got)
	}
}

func TestRenderKeepsCodeLiteral(t *testing.T) {
	t.Parallel()

	got := Render("```tex\n\\frac{x}{y}\n```\n`\\pi`")

	for _, want := range []string{`\frac{x}{y}`, `⦗ \pi ⦘`} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render(code) = %q, want %q", got, want)
		}
	}
}

func TestRenderTableKeepsPipeInsideInlineCode(t *testing.T) {
	t.Parallel()

	got := Render("| Expr | Value |\n| --- | --- |\n| `x|y` | ok |")

	for _, want := range []string{"【Expr】", "《1》 ⦗ x|y ⦘", "【Value】", "《1》 ok"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render(table) = %q, want %q", got, want)
		}
	}
}

func TestRenderWrappedCodeBlock(t *testing.T) {
	t.Parallel()

	got := Render("````\n```js\nconst x=1\n```\n````")
	want := "┏━━━━━ js ━━━━━┓\n```\nconst x=1\n```\n┗━━━━━━━━━━━┛"

	if got != want {
		t.Fatalf("Render(wrapped) = %q, want %q", got, want)
	}
}

func TestRenderKeepsMalformedThenConvertsValidBold(t *testing.T) {
	t.Parallel()

	got := Render("**bold__ and __ok__")
	if !strings.Contains(got, "**bold__") {
		t.Fatalf("Render() = %q, want raw malformed delimiter", got)
	}

	if !strings.Contains(got, "𝗼𝗸") && !strings.Contains(got, "❪𝗼𝗸❫") {
		t.Fatalf("Render() = %q, want converted valid bold", got)
	}
}

func TestRenderDoesNotKeepMarkdownTokens(t *testing.T) {
	t.Parallel()

	got := Render("# 제목\n- **강조**\n> 인용")

	for _, want := range []string{"【제목】", "❪강조❫", "  ‖ 인용"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render() = %q, want %q", got, want)
		}
	}

	if strings.Contains(got, "**【제목】**") {
		t.Fatalf("Render() kept kakao markdown heading: %q", got)
	}
}

func TestPrevRuneIsConstantTime(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("가나다라마", 100000) + "*"
	byteIndex := len(text) - len("*")
	last, _ := utf8.DecodeLastRuneInString(text[:byteIndex])

	if got := prevRune(text, byteIndex); got != last {
		t.Fatalf("prevRune = %q, want %q", got, last)
	}
}

func TestEmphasisRenderingBoundedTime(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("*굵게* 그리고 _기울임_ 텍스트 ", 20000)
	done := make(chan struct{})

	go func() {
		_ = renderEmphasis(input)

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("emphasis rendering did not complete in time")
	}
}

func TestTableOutputAmplificationCapped(t *testing.T) {
	t.Parallel()

	var sb strings.Builder

	header := "|" + strings.Repeat(" col |", 100)
	sep := "|" + strings.Repeat(" --- |", 100)
	sb.WriteString(header + "\n" + sep + "\n")

	for range 1000 {
		sb.WriteString("|" + strings.Repeat(" v |", 100) + "\n")
	}

	output := renderTables(sb.String())
	lineCount := strings.Count(output, "\n") + 1

	if lineCount > maxTableOutputLines+10 {
		t.Fatalf("table rendering produced %d lines, want <= %d", lineCount, maxTableOutputLines+10)
	}
}

func TestLineRenderersLeavePlainText(t *testing.T) {
	t.Parallel()

	if got := renderLine("plain text"); got != "plain text" {
		t.Fatalf("renderLine() = %q", got)
	}
}
