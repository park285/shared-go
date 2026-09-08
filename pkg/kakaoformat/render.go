// Package kakaoformat은 Markdown을 카카오 일반챗처럼 문법을 렌더하지 않는
// 화면에 맞춰 유니코드 스타일 평문으로 바꿉니다.
package kakaoformat

import (
	"strings"
	"unicode/utf8"
)

// Render는 Markdown 강조·제목·목록·링크·표를 유니코드 평문으로 바꿉니다.
// NUL 또는 잘못된 UTF-8이 있으면 입력을 그대로 반환합니다.
// 표의 행·열·표시 확장 한도를 넘으면 해당 표 원문 전체를 보존합니다.
func Render(input string) string {
	if strings.TrimSpace(input) == "" || strings.ContainsRune(input, 0) || !utf8.ValidString(input) {
		return input
	}

	return strings.TrimSpace(render(input))
}
