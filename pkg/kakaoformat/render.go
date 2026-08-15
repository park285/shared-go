// Package kakaoformat은 Markdown을 카카오 일반챗처럼 문법을 렌더하지 않는
// 화면에 맞춰 유니코드 스타일 평문으로 바꿉니다.
package kakaoformat

import "strings"

// Render는 Markdown 강조·제목·목록·링크·표를 유니코드 평문으로 바꿉니다.
func Render(input string) string {
	if strings.TrimSpace(input) == "" {
		return input
	}

	return strings.TrimSpace(render(input))
}

func render(input string) string {
	code := newStore("CODE")
	inline := newStore("INLINE")

	text := protectWrappedCode(input, code)
	text = protectCodeBlocks(text, code)
	text = protectInlineCode(text, inline)
	text = renderLines(text)
	text = renderTables(text)
	text = renderLinks(text)
	text = renderEmphasis(text)
	text = renderStrike(text)
	text = inline.Restore(text)
	text = cleanupSpacing(text)

	return code.Restore(text)
}
