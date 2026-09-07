// Package kakaoformat은 Markdown을 카카오 일반챗처럼 문법을 렌더하지 않는
// 화면에 맞춰 유니코드 스타일 평문으로 바꿉니다.
package kakaoformat

import "strings"

// Render는 Markdown 강조·제목·목록·링크·표를 유니코드 평문으로 바꿉니다.
func Render(input string) string {
	if strings.TrimSpace(input) == "" || strings.ContainsRune(input, 0) {
		return input
	}

	return strings.TrimSpace(render(input))
}

func render(input string) string {
	code := newStore("CODE")
	inline := newStore("INLINE")
	urls := newStore("URL")

	text := protectCodeRanges(input, code)

	text = TransformEscapes(text, inline.Put)
	text = renderLinks(text, urls)
	text = renderLines(text)
	text = renderTables(text)
	text = renderEmphasis(text)
	text = renderStrike(text)
	text = cleanupSpacing(text)
	text = urls.Restore(text)
	text = inline.Restore(text)

	return code.Restore(text)
}
