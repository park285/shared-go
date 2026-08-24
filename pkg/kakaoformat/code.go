package kakaoformat

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	reWrappedCode = regexp.MustCompile("(?ms)^([ \t]*)`{4,}\n(.*?)\n`{4,}[ \t]*")
	reCodeBlock   = regexp.MustCompile("(?ms)^([ \t]*)```([^\n`]*)\n(.*?)\n```[ \t]*")
	reInlineCode  = regexp.MustCompile("`([^`\n]+)`")
)

func protectWrappedCode(input string, dst *store) string {
	return reWrappedCode.ReplaceAllStringFunc(input, func(match string) string {
		parts := reWrappedCode.FindStringSubmatch(match)
		indent := parts[1]
		body := strings.Trim(parts[2], "\n")
		lang := "Code"
		display := body

		if inner := reCodeBlock.FindStringSubmatch(body); len(inner) > 0 {
			if name := strings.TrimSpace(inner[2]); name != "" {
				lang = name
			}

			display = "```\n" + strings.TrimRight(inner[3], "\n") + "\n```"
		}

		return dst.Put(codeBox(indent, lang, display))
	})
}

func protectCodeBlocks(input string, dst *store) string {
	return reCodeBlock.ReplaceAllStringFunc(input, func(match string) string {
		parts := reCodeBlock.FindStringSubmatch(match)
		lang := strings.TrimSpace(parts[2])

		if lang == "" {
			lang = "Code"
		}

		return dst.Put(codeBox(parts[1], lang, strings.TrimRight(parts[3], "\n")))
	})
}

func protectInlineCode(input string, dst *store) string {
	return reInlineCode.ReplaceAllStringFunc(input, func(match string) string {
		parts := reInlineCode.FindStringSubmatch(match)

		return dst.Put("⦗ " + parts[1] + " ⦘")
	})
}

func codeBox(indent, lang, body string) string {
	bottom := 10 + (utf8.RuneCountInString(lang)+1)/2

	var b strings.Builder

	b.Grow(len(indent)*2 + len(lang) + len(body) + bottom + 24)
	b.WriteByte('\n')
	b.WriteString(indent)
	b.WriteString("┏━━━━━ ")
	b.WriteString(lang)
	b.WriteString(" ━━━━━┓\n")
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(indent)
	b.WriteString("┗")
	b.WriteString(strings.Repeat("━", bottom))
	b.WriteString("┛\n")

	return b.String()
}
