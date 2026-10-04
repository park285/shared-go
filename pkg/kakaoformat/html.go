package kakaoformat

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
)

var (
	reBreakTag      = regexp.MustCompile(`(?i)^<br\s*/?>$`)
	reScriptOpenTag = regexp.MustCompile(`(?i)^<(sup|sub)>$`)
)

var (
	htmlSuperscripts = scriptMap("0123456789+-=()in", "⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾ⁱⁿ")
	htmlSubscripts   = scriptMap("0123456789+-=()", "₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎")
)

// rawHTMLText는 인라인 HTML 중 화면 의미가 분명한 줄바꿈과 주석만 평문으로 바꾸고 나머지 태그는 원문으로 둔다.
func rawHTMLText(value string) string {
	switch {
	case reBreakTag.MatchString(value):
		return "\n"
	case strings.HasPrefix(value, "<!--"):
		return ""
	default:
		return value
	}
}

// scriptTag는 <sup>·<sub>와 짝이 맞는 닫는 태그 사이를 위·아래 첨자로 바꾼다.
// 짝이 없거나 첨자로 표시할 수 없는 문자는 ^(...)·_(...)로 남겨 지수 의미를 잃지 않게 한다.
func (d *plainDocument) scriptTag(node ast.Node, style int) (ast.Node, string, bool) {
	open, ok := node.(*ast.RawHTML)
	if !ok {
		return nil, "", false
	}

	match := reScriptOpenTag.FindStringSubmatch(open.Value.Value(d.source))
	if match == nil {
		return nil, "", false
	}

	name := strings.ToLower(match[1])

	var body strings.Builder

	for current := node.NextSibling(); current != nil; current = current.NextSibling() {
		if closing, ok := current.(*ast.RawHTML); ok && strings.EqualFold(closing.Value.Value(d.source), "</"+name+">") {
			return current, scriptText(body.String(), name), true
		}

		d.inlineNode(&body, current, style)
	}

	return nil, "", false
}

func scriptText(value, name string) string {
	mapping, marker := htmlSuperscripts, "^"

	if name == "sub" {
		mapping, marker = htmlSubscripts, "_"
	}

	var output strings.Builder

	for _, char := range value {
		mapped, ok := mapping[char]
		if !ok {
			return marker + "(" + value + ")"
		}

		output.WriteRune(mapped)
	}

	return output.String()
}

func scriptMap(keys, values string) map[rune]rune {
	result := make(map[rune]rune)
	mapped := []rune(values)

	for index, key := range []rune(keys) {
		result[key] = mapped[index]
	}

	return result
}
