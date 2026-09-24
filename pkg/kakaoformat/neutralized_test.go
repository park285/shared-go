package kakaoformat

import (
	"strings"
	"testing"
)

func TestRenderPreservesZeroWidthNeutralizedMarkers(t *testing.T) {
	t.Parallel()

	neutralizer := strings.NewReplacer("*", "*\u200b", "_", "_\u200b", "`", "`\u200b", "~", "~\u200b", "]", "]\u200b", "#", "#\u200b", "&", "&\u200b", "<", "<\u200b", "\\", "\\\u200b")

	for _, literal := range []string{
		"**미코 Miko** _노래_ ~~오늘~~",
		"`노래 방송`과 ```코드 아닌 제목```",
		"[방송 제목](https://youtu.be/a_b#c)",
		"# 제목의 해시태그 #생방송",
		"한글_日本語_English*stars*",
		"Fish &amp; Chips <someone@example.invalid>",
		`C:\stream\\recording`,
	} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()

			input := neutralizer.Replace(literal)
			if got := Render(input); got != input {
				t.Errorf("neutralized text reinterpreted: got=%q want=%q", got, input)
			}
		})
	}
}

func TestRenderNeutralizedTextKeepsMarkdownCodeURLsAndFoldPadding(t *testing.T) {
	t.Parallel()

	const (
		literal = "*\u200b*\u200b제목*\u200b*\u200b"
		url     = "https://youtu.be/a_b#c"
	)

	padding := strings.Repeat("\u200b", 500)
	input := "**Live**\n" + padding + "\n" + literal + "\n" + url + "\n\n`" + literal + "`"
	got := Render(input)

	for _, want := range []string{"𝗟𝗶𝘃𝗲", padding, literal, "\n" + url + "\n", "⦗ " + literal + " ⦘"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost existing rendering contract %q: %q", want, got)
		}
	}

	if strings.ContainsRune(got, 0) {
		t.Fatal("internal placeholder leaked")
	}
}

func TestRepeatedNeutralizedTextKeepsCodeAndLinkBoundaries(t *testing.T) {
	t.Parallel()

	const literal = "*\u200b_\u200b`\u200b~\u200b]\u200b#\u200b&\u200b<\u200b\\\u200b"

	input := strings.Repeat(literal+"\n", 100) + "\n`" + literal + "`\n[링크](https://youtu.be/a_b#c)\n" + literal
	output := Render(input)

	if strings.Count(output, literal) != 102 || !strings.Contains(output, "⦗ "+literal+" ⦘") ||
		!strings.Contains(output, "링크( https://youtu.be/a_b#c )") || strings.ContainsRune(output, 0) {
		t.Fatalf("repeated literal changed code or link rendering: %q", output)
	}
}
