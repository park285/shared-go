package kakaoformat

import "testing"

func TestRenderPlainTextPreservesOriginalCharacters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, input, want string }{
		{"ascii styles", "**Hello 123** *world 456* ***ABC xyz*** ~~removed~~", "Hello 123 world 456 ABC xyz removed"},
		{"korean punctuation", "**'응급환자 이송(구급차)'**과 **'국가 원수 경호 및 안보(대통령 탑승)'**라는 목적", "'응급환자 이송(구급차)'과 '국가 원수 경호 및 안보(대통령 탑승)'라는 목적"},
		{"nested styles", "**한글 *English 123* ~~취소~~**", "한글 English 123 취소"},
		{"existing unicode", "𝗔𝟭 ❪강조❫ 취\u0336소\u0336 ⦗ code ⦘ ┏━┓", "𝗔𝟭 ❪강조❫ 취\u0336소\u0336 ⦗ code ⦘ ┏━┓"},
		{"math meaning", "**H<sub>2</sub>O**와 *x<sup>2</sup>*, 𝑥² + 𝒚", "H₂O와 x², 𝑥² + 𝒚"},
		{"emoji and literal command", "🚑 **@고슴도치**: `!검색 **원문**`", "🚑 @고슴도치: !검색 **원문**"},
		{"escaped markdown", "\\*\\*원문\\*\\*과 `~~코드~~`", "**원문**과 ~~코드~~"},
		{"sources", "**설명**입니다.\n\n출처: [법령](https://law.go.kr/a_b) · https://news.example/a*b*", "설명입니다.\n\n출처: 법령( https://law.go.kr/a_b ) · https://news.example/a*b*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderPlainCodePreservesWhitespaceAndContainers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, input, want string }{
		{"inline padding", "`  x  `", "  x  "},
		{"inline lines", "`x\n\n`", "`x\n\n`"},
		{"fence whitespace", "```go\n  x := 1  \n\n\tprintln(x)\t\n```", "  x := 1  \n\n\tprintln(x)\t\n"},
		{"nested fence", "````text\n```go\n**literal**\n```\n````", "```go\n**literal**\n```\n"},
		{"indented block", "    **literal**  \n    [x](y)\t", "    **literal**  \n    [x](y)\t"},
		{"quoted fence", "> ```go\n> **literal**  \n> x\t\n> ```", "> **literal**  \n> x\t"},
		{"list fence", "- ```go\n  **literal**  \n  x\t\n  ```", "- **literal**  \n  x\t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderPlainListsKeepTaskStatusAndStartingNumber(t *testing.T) {
	t.Parallel()

	input := "3. [x] 완료\n4. [ ] 대기\n   - **하위**\n     - *다음*"
	want := "3. [x] 완료\n4. [ ] 대기\n  - 하위\n    - 다음"
	if got := Render(input); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderPlainTableKeepsRowsAndMultilineCells(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"| 사람 | 상태 |\n| --- | --- |\n| **김** | 완료<br>확인 |\n| *이* | 대기 |", "사람 | 상태\n김 | 완료\n | 확인\n이 | 대기"},
		{"| H |\n| --- |\n| |\n| value |\n|", "H\n|\nvalue\n|"},
	} {
		if got := Render(tc.input); got != tc.want {
			t.Fatalf("Render() = %q, want %q", got, tc.want)
		}
	}
}
