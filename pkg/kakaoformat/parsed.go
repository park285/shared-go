package kakaoformat

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	emphasisItalic = 1
	emphasisBold   = 2
	emphasisStrike = 4
)

var (
	plainParser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList), goldmark.WithParserOptions(parser.WithInlineParsers(util.Prioritized(&literalURLParser{}, 150)))).Parser()
	plainEntity = regexp.MustCompile(`&(?:#[xX][0-9a-fA-F]{1,6}|#[0-9]{1,7}|[A-Za-z][A-Za-z0-9]{1,31});`)
)

type (
	spanShape     struct{ italic, bold bool }
	plainDocument struct {
		source     []byte
		shapes     map[ast.Node]spanShape
		tableLines int
	}
)

func render(input string) string {
	code := newStore("CODE")
	source := []byte(protectCodeRanges(input, code))
	root := plainParser.Parse(text.NewReader(source))
	document := plainDocument{source: source, shapes: make(map[ast.Node]spanShape)}
	document.measure(root)

	return code.Restore(cleanupSpacing(document.blocks(root, 0)))
}

func (d *plainDocument) measure(node ast.Node) spanShape {
	// 중첩 강조마다 본문을 다시 복사하지 않도록 변환 가능 여부를 한 번씩 계산한다.
	shape := spanShape{true, true}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		part := d.measure(child)

		shape.italic = shape.italic && part.italic
		shape.bold = shape.bold && part.bold
	}

	switch n := node.(type) {
	case *ast.Text:
		value := plainText(string(n.Segment.Value(d.source)), n.IsRaw())

		shape = spanShape{convertible(value, false), convertible(value, true)}
	case *ast.String:
		value := plainText(string(n.Value), n.IsRaw())

		shape = spanShape{convertible(value, false), convertible(value, true)}
	case *ast.Link, *ast.Image, *ast.AutoLink, *ast.CodeSpan, *ast.RawHTML, *literalURL:
		shape = spanShape{}
	}

	d.shapes[node] = shape

	return shape
}

func (d *plainDocument) blocks(parent ast.Node, level int) string {
	var (
		output   strings.Builder
		previous ast.Node
	)

	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		value := d.block(node, level)
		if value == "" {
			continue
		}

		if output.Len() > 0 {
			if node.HasBlankPreviousLines() || node.Kind() == ast.KindHeading || previous.Kind() == ast.KindHeading {
				output.WriteString("\n\n")
			} else {
				output.WriteByte('\n')
			}
		}

		output.WriteString(value)

		previous = node
	}

	return output.String()
}

func (d *plainDocument) block(node ast.Node, level int) string {
	switch n := node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return d.inline(node, 0)
	case *ast.Heading:
		return "【" + d.inline(node, 0) + "】"
	case *ast.ThematicBreak:
		return strings.Repeat("━", 20)
	case *ast.Blockquote:
		return prefixLines(d.blocks(node, level), "  ‖ ")
	case *ast.List:
		return d.list(n, level)
	case *extast.Table:
		return d.table(n)
	case *ast.FencedCodeBlock:
		body := strings.TrimSuffix(string(n.Lines().Value(d.source)), "\n")
		language := string(n.Language(d.source))

		if language == "" {
			language = "Code"
		}

		return strings.TrimSpace(codeBox("", language, body))
	case *ast.CodeBlock:
		return string(n.Lines().Value(d.source))
	case *ast.HTMLBlock:
		value := string(n.Lines().Value(d.source))
		if n.HasClosure() {
			value += string(n.ClosureLine.Value(d.source))
		}

		return strings.TrimSuffix(value, "\n")
	default:
		return d.blocks(node, level)
	}
}

func prefixLines(input, prefix string) string {
	var output strings.Builder

	for index, line := range strings.Split(input, "\n") {
		if index > 0 {
			output.WriteByte('\n')
		}

		if line != "" {
			output.WriteString(prefix)
		}

		output.WriteString(line)
	}

	return output.String()
}

func (d *plainDocument) list(node *ast.List, level int) string {
	var output strings.Builder

	number := node.Start

	for item := node.FirstChild(); item != nil; item = item.NextSibling() {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}

		marker := bulletFor(level) + " "

		if node.IsOrdered() {
			marker = strconv.Itoa(number) + ". "
			number++
		}

		first := item.FirstChild()
		if first != nil && first.FirstChild() != nil && first.FirstChild().Kind() == extast.KindTaskCheckBox {
			marker = ""
		}

		output.WriteString(listIndent(level) + marker)

		for child := first; child != nil; child = child.NextSibling() {
			if child != first {
				output.WriteByte('\n')

				if child.HasBlankPreviousLines() {
					output.WriteByte('\n')
				}
			}

			value := d.block(child, level+1)
			if child != first && child.Kind() != ast.KindList {
				value = prefixLines(value, listIndent(level+1))
			}

			output.WriteString(value)
		}
	}

	return output.String()
}

func (d *plainDocument) inline(parent ast.Node, style int) string {
	var output strings.Builder

	d.inlines(&output, parent, style)

	return output.String()
}

func (d *plainDocument) inlines(output *strings.Builder, parent ast.Node, style int) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		d.inlineNode(output, child, style)
	}
}

func (d *plainDocument) inlineNode(output *strings.Builder, node ast.Node, style int) {
	switch n := node.(type) {
	case *ast.Text:
		output.WriteString(styledText(plainText(string(n.Segment.Value(d.source)), n.IsRaw()), style))

		if n.SoftLineBreak() || n.HardLineBreak() {
			output.WriteByte('\n')
		}
	case *ast.String:
		output.WriteString(styledText(plainText(string(n.Value), n.IsRaw()), style))
	case *ast.Emphasis:
		d.emphasis(output, n, style)
	case *extast.Strikethrough:
		d.inlines(output, node, style|emphasisStrike)
	case *ast.Link:
		d.link(output, node, string(n.Destination), style)
	case *ast.Image:
		d.link(output, node, string(n.Destination), style)
	case *literalURL:
		output.WriteString(TransformEscapes(string(n.segment.Value(d.source)), func(value string) string { return value }))
	case *ast.AutoLink:
		output.Write(n.Label(d.source))
	case *ast.CodeSpan:
		var body strings.Builder

		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if value, ok := child.(*ast.Text); ok {
				body.Write(value.Segment.Value(d.source))
			}
		}

		output.WriteString("⦗ " + body.String() + " ⦘")
	case *ast.RawHTML:
		output.Write(n.Segments.Value(d.source))
	case *extast.TaskCheckBox:
		if n.IsChecked {
			output.WriteString("✔ ")
		} else {
			output.WriteString("✖ ")
		}
	default:
		d.inlines(output, node, style)
	}
}

func (d *plainDocument) emphasis(output *strings.Builder, node *ast.Emphasis, inherited int) {
	style := node.Level
	current := ast.Node(node)

	for current.ChildCount() == 1 {
		nested, ok := current.FirstChild().(*ast.Emphasis)
		if !ok {
			break
		}

		style |= nested.Level

		current = nested
	}

	shape := d.shapes[node]
	convertibleSpan := shape.italic

	if style&emphasisBold != 0 {
		convertibleSpan = shape.bold
	}

	open, end := "", ""

	if !convertibleSpan && style&^inherited != 0 {
		switch style {
		case emphasisItalic:
			open, end = "❬", "❭"
		case emphasisBold:
			open, end = "❪", "❫"
		default:
			open, end = "❮", "❯"
		}
	}

	output.WriteString(open)
	d.inlines(output, current, inherited|style)
	output.WriteString(end)
}

func (d *plainDocument) link(output *strings.Builder, node ast.Node, destination string, style int) {
	label := d.inline(node, style)

	destination = plainText(destination, false)

	if label == "" || strings.EqualFold(d.inline(node, 0), destination) {
		output.WriteString(destination)
	} else if destination == "" {
		output.WriteString(label)
	} else {
		output.WriteString(label + "( " + destination + " )")
	}
}

func styledText(input string, style int) string {
	switch style & (emphasisItalic | emphasisBold) {
	case emphasisItalic:
		input = styleItalic(input)
	case emphasisBold:
		input = styleBold(input)
	case emphasisBold | emphasisItalic:
		input = styleBoldItalic(input)
	}

	if style&emphasisStrike != 0 {
		input = mapOutsidePlaceholders(input, strikeText)
	}

	return input
}

func plainText(input string, raw bool) string {
	if raw {
		return input
	}

	var output strings.Builder

	start := 0

	for index := 0; index < len(input); index++ {
		if input[index] == '\\' && index+1 < len(input) && markdownPunctuation(input[index+1]) {
			output.WriteString(plainEntity.ReplaceAllStringFunc(input[start:index], html.UnescapeString))
			output.WriteByte(input[index+1])

			index++

			start = index + 1
		}
	}

	output.WriteString(plainEntity.ReplaceAllStringFunc(input[start:], html.UnescapeString))

	return output.String()
}

func (d *plainDocument) table(node *extast.Table) string {
	source := d.tableSource(node)
	headers := node.FirstChild()
	columns, rows := headers.ChildCount(), node.ChildCount()-1

	if columns > maxTableColumns || rows > maxTableRows || columns*(rows+2) > maxTableOutputLines-d.tableLines {
		// 표시 확장 예산을 넘겨도 행·열을 버리지 않고 표 원문 전체를 남긴다.
		return source
	}

	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		start, end := row.Pos(), row.Pos()
		for end < len(d.source) && d.source[end] != '\n' {
			end++
		}

		// 행의 위치는 인용·목록 접두사를 제외하며, 파서가 생략한 추가 셀도 원문에서 확인한다.
		if tableColumnCount(string(d.source[start:end])) > columns {
			return source
		}
	}

	d.tableLines += columns * (rows + 2)

	values := make([][]string, 0, rows+1)

	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		cells := make([]string, 0, columns)

		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, d.inline(cell, 0))
		}

		values = append(values, cells)
	}

	var output strings.Builder

	for column, name := range values[0] {
		if column > 0 {
			output.WriteByte('\n')
		}

		if name == "" {
			name = "열 " + strconv.Itoa(column+1)
		}

		output.WriteString("【" + name + "】\n")

		for row := 1; row < len(values); row++ {
			output.WriteString("    《" + strconv.Itoa(row) + "》 " + values[row][column] + "\n")
		}

		output.WriteString("-------------------------")
	}

	return output.String()
}

func (d *plainDocument) tableSource(node *extast.Table) string {
	start := node.Pos()
	end := start

	for row := node.FirstChild(); row != nil; row = row.NextSibling() {
		end = max(end, row.Pos())

		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			lines := cell.Lines()
			if lines.Len() > 0 {
				end = max(end, lines.At(lines.Len()-1).Stop)
			}
		}
	}

	for start > 0 && d.source[start-1] != '\n' {
		start--
	}

	for end < len(d.source) && d.source[end] != '\n' {
		end++
	}

	return string(d.source[start:end])
}
