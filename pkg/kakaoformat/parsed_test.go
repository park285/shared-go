package kakaoformat

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMarkdownStructureRegressions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"**굵게 *기울임***", "❪굵게 ❬기울임❭❫"},
		{"**bold *italic* end**", "𝗯𝗼𝗹𝗱 𝙞𝙩𝙖𝙡𝙞𝙘 𝗲𝗻𝗱"},
		{"*italic **bold** end*", "𝘪𝘵𝘢𝘭𝘪𝘤 𝙗𝙤𝙡𝙙 𝘦𝘯𝘥"},
		{"**a **b** c**", "𝗮 𝗯 𝗰"},
		{"***a** b*", "𝙖 𝘣"},
		{"~~a ~~b~~ c~~", "a̶ ̶b̶ ̶c̶"},
		{"**미완성\n\n다음**", "**미완성\n\n다음**"},
		{"**미완성\n- 항목**", "**미완성\n⦁ 항목**"},
		{"[설명 [상세]](https://example.com)", "설명 [상세]( https://example.com )"},
		{"[제목](https://example.com \"설명\")", "제목( https://example.com )"},
		{"[제목](<https://example.com/a_(b)>)", "제목( https://example.com/a_(b) )"},
		{"[제목][id]\n\n[id]: https://example.com", "제목( https://example.com )"},
		{"[제목]()", "제목"},
		{"<https://example.com/a_b>", "https://example.com/a_b"},
		{"[**강조**](https://example.com)", "❪강조❫( https://example.com )"},
		{"  # 제목 ###", "【제목】"},
		{"제목\n===", "【제목】"},
		{"> # 제목\n> - **강조**", "‖ 【제목】\n\n  ‖ ⦁ ❪강조❫"},
		{"+ [x] 완료", "✔ 완료"},
		{"1) 첫째\n2) 둘째", "1. 첫째\n2. 둘째"},
		{"문장  \n다음", "문장\n다음"},
		{"이름 | 금액\n--- | ---\n엔 | 100", "【이름】\n    《1》 엔\n-------------------------\n【금액】\n    《1》 100\n-------------------------"},
		{"| | 금액 |\n| --- | --- |\n| 엔 | 100 |", "【열 1】\n    《1》 엔\n-------------------------\n【금액】\n    《1》 100\n-------------------------"},
		{"| A | B |\n| --- | --- |\n| **first | second** |", "【A】\n    《1》 **first\n-------------------------\n【B】\n    《1》 second**\n-------------------------"},
		{"| A | B |\n| --- | |\n| first | second |", "| A | B |\n| --- | |\n| first | second |"},
		{"| A | B |\n| --- | --- |\n| first | second | third |", "| A | B |\n| --- | --- |\n| first | second | third |"},
		{"| A | B |\n| --- | --- |\n| first | `second | third |", "| A | B |\n| --- | --- |\n| first | `second | third |"},
		{"> | A |\n> | --- |\n> | value |", "‖ 【A】\n  ‖     《1》 value\n  ‖ -------------------------"},
		{"**a &amp; b**", "𝗮 & 𝗯"},
		{"a \\&amp; b", "a &amp; b"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOversizedTablesPreserveAllData(t *testing.T) {
	t.Parallel()

	for _, columns := range []int{2, maxTableColumns + 1} {
		var input strings.Builder

		for column := range columns {
			fmt.Fprintf(&input, "| H%d ", column)
		}

		input.WriteString("|\n" + strings.Repeat("| --- ", columns) + "|\n")

		for row := range maxTableRows + 1 {
			for column := range columns {
				fmt.Fprintf(&input, "| R%dC%d ", row, column)
			}

			input.WriteString("|\n")
		}

		want := strings.TrimSpace(input.String())
		if got := Render(input.String()); got != want {
			t.Fatal("oversized table was truncated or changed")
		}
	}
}

func TestTableBudgetPreservesLaterTables(t *testing.T) {
	t.Parallel()

	table := "|" + strings.Repeat(" name |", 10) + "\n|" + strings.Repeat(" --- |", 10) + "\n" +
		strings.Repeat("|"+strings.Repeat(" value |", 10)+"\n", 199)
	got := Render(table + "\n" + table)

	if !strings.Contains(got, "《199》 value") || !strings.HasSuffix(got, strings.TrimSpace(table)) {
		t.Fatal("table expansion budget lost the later table")
	}
}

func TestOversizedTableKeepsEmptyLastRow(t *testing.T) {
	t.Parallel()

	input := "| A |\n| --- |\n" + strings.Repeat("| value |\n", maxTableRows) + "|"
	if got := Render(input); got != input {
		t.Fatal("oversized table lost its empty last row")
	}
}

func FuzzRenderMarkdown(f *testing.F) {
	for _, input := range []string{"**강조**입니다.", "**굵게 *기울임***", "https://example.com/a*b*", "[x](url)", "| A |\n|---|\n| b |", "\x00CODE0\x00", string([]byte{0xff})} {
		f.Add(input)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}

		got := Render(input)
		if !utf8.ValidString(input) || strings.ContainsRune(input, 0) {
			if got != input {
				t.Fatal("invalid input changed")
			}
		} else if !utf8.ValidString(got) || strings.ContainsRune(got, 0) {
			t.Fatalf("invalid output: %q", got)
		}
	})
}

func TestMarkdownEscapedLiteralRoundTrip(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"&amp;", "1) item", "0)", "0.", "a &#42; b", "https://example.com/a*b*?x=&amp;", "<tag>literal</tag>", "__literal__", "[a](https://example.com/a_(b))"} {
		if got := Render(EscapeMarkdown(input)); got != input {
			t.Errorf("input=%q got=%q", input, got)
		}
	}
}

func FuzzMarkdownEscapedLiteral(f *testing.F) {
	for _, input := range []string{"&amp;", "1) item", "https://example.com/a*b*?x=&amp;", "__literal__"} {
		f.Add(input)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 || strings.ContainsAny(input, "\r\n\x00") || !utf8.ValidString(input) {
			t.Skip()
		}

		escaped := EscapeMarkdown(input)
		// EscapeMarkdown은 코드 구간 밖에 삽입하는 계약이므로 들여쓰기 코드 문서는 제외한다.
		for _, code := range CodeRanges(escaped) {
			if code.Block {
				t.Skip()
			}
		}

		want := strings.TrimSpace(input)
		if want == "" {
			want = input
		}

		if got := Render(escaped); got != want {
			t.Fatalf("input=%q got=%q want=%q", input, got, want)
		}
	})
}
