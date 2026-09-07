package kakaoformat

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	reCodeBlock  = regexp.MustCompile("(?ms)^([ \t]*)```([^\n`]*)\n(.*?)\n```[ \t]*")
	reInlineCode = regexp.MustCompile("`([^`\n]+)`")
)

func protectCodeRanges(input string, dst *store) string {
	var output strings.Builder

	last := 0
	destinations := literalDestinations(input)
	destination := 0

	for _, code := range CodeRanges(input) {
		for destination < len(destinations) && destinations[destination][1] <= code.Start {
			destination++
		}

		if !code.Block && destination < len(destinations) && destinations[destination][0] < code.Start && code.End <= destinations[destination][1] {
			continue
		}

		output.WriteString(input[last:code.Start])

		value := formatCodeRange(input, code)

		output.WriteString(dst.Put(strings.TrimSuffix(value, "\n")))

		if strings.HasSuffix(input[code.Start:code.End], "\n") {
			output.WriteByte('\n')
		}

		last = code.End
	}

	output.WriteString(input[last:])

	return output.String()
}

func formatCodeRange(input string, code CodeRange) string {
	if !code.Block {
		return "⦗ " + input[code.BodyStart:code.BodyEnd] + " ⦘"
	}

	if !code.Fenced || code.Container {
		return input[code.Start:code.End]
	}

	lang := code.Language
	if lang == "" {
		lang = "Code"
	}

	body := strings.TrimRight(input[code.BodyStart:code.BodyEnd], "\r\n")
	if code.Width >= 4 {
		if inner := reCodeBlock.FindStringSubmatch(body); len(inner) > 0 {
			if name := strings.TrimSpace(inner[2]); name != "" {
				lang = name
			}

			body = "```\n" + strings.TrimRight(inner[3], "\n") + "\n```"
		}
	}

	return codeBox(code.Indent, lang, body)
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
