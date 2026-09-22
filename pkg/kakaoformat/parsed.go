package kakaoformat

import (
	"iter"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

const (
	emphasisItalic = 1
	emphasisBold   = 2
	emphasisStrike = 4
)

var plainParser = parser.New(
	parser.WithExtensions(extension.TableParser, extension.StrikethroughParser, extension.TaskListItemParser),
	parser.WithInlineParsers(util.Prioritized[parser.InlineParser](&literalURLParser{}, 150)),
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
	root := plainParser.Parse(source)
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
		value := n.Value.Value(d.source)

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

		if previous != nil {
			if blockNode(node).HasBlankPreviousLines() || node.Kind() == ast.KindHeading || previous.Kind() == ast.KindHeading {
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
	case *ast.Paragraph:
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
	case *ast.CodeBlock:
		if n.CodeBlockKind == ast.CodeBlockKindIndented {
			return n.Value.Str(d.source)
		}

		body := strings.TrimSuffix(n.Value.Str(d.source), "\n")
		language, _ := n.Language(d.source)

		if language == "" {
			language = "Code"
		}

		return strings.TrimSpace(codeBox("", language, body))
	case *ast.HTMLBlock:
		return strings.TrimSuffix(n.Value.Str(d.source), "\n")
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
		if status, ok := extension.TaskStatusOf(item); ok {
			marker = "✖ "

			if status == extension.TaskStatusCompleted {
				marker = "✔ "
			}
		}

		output.WriteString(listIndent(level) + marker)

		for child := first; child != nil; child = child.NextSibling() {
			if child != first {
				output.WriteByte('\n')

				if blockNode(child).HasBlankPreviousLines() {
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
		output.WriteString(styledText(n.Value.Value(d.source), style))

		if n.SoftLineBreak() || n.HardLineBreak() {
			output.WriteByte('\n')
		}
	case *ast.Emphasis:
		d.emphasis(output, n, emphasisItalic, style)
	case *ast.Strong:
		d.emphasis(output, n, emphasisBold, style)
	case *extast.Strikethrough:
		d.inlines(output, node, style|emphasisStrike)
	case *ast.Link:
		d.link(output, node, n.Destination.Value(d.source), style)
	case *ast.Image:
		d.link(output, node, n.Destination.Value(d.source), style)
	case *literalURL:
		output.WriteString(TransformEscapes(n.segment.Str(d.source), func(value string) string { return value }))
	case *ast.AutoLink:
		output.WriteString(n.Label.Value(d.source))
	case *ast.CodeSpan:
		output.WriteString("⦗ " + n.Value.Value(d.source) + " ⦘")
	case *ast.RawHTML:
		output.WriteString(n.Value.Value(d.source))
	default:
		d.inlines(output, node, style)
	}
}

func (d *plainDocument) emphasis(output *strings.Builder, node ast.Node, style, inherited int) {
	current := node

	for current.ChildCount() == 1 {
		nested := current.FirstChild()
		switch nested.(type) {
		case *ast.Emphasis:
			style |= emphasisItalic
		case *ast.Strong:
			style |= emphasisBold
		default:
			nested = nil
		}

		if nested == nil {
			break
		}

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

func blockNode(node ast.Node) ast.BlockNode {
	block, ok := node.(ast.BlockNode)
	if !ok {
		panic("kakaoformat: non-block node in block traversal")
	}

	return block
}

func tableRows(node *extast.Table) iter.Seq[ast.Node] {
	return func(yield func(ast.Node) bool) {
		header := node.FirstChild()
		if !yield(header) {
			return
		}

		if body := header.NextSibling(); body != nil {
			for row := range body.Children() {
				if !yield(row) {
					return
				}
			}
		}
	}
}

func (d *plainDocument) table(node *extast.Table) string {
	source := d.tableSource(node)
	headers := node.FirstChild()
	columns, rows := headers.ChildCount(), 0

	if body := headers.NextSibling(); body != nil {
		rows = body.ChildCount()
	}

	if columns > maxTableColumns || rows > maxTableRows || columns*(rows+2) > maxTableOutputLines-d.tableLines {
		// 표시 확장 예산을 넘겨도 행·열을 버리지 않고 표 원문 전체를 남긴다.
		return source
	}

	for row := range tableRows(node) {
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

	for row := range tableRows(node) {
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

	for row := range tableRows(node) {
		end = max(end, row.Pos())

		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			lines := blockNode(cell).Source()
			if len(lines) > 0 {
				end = max(end, lines[len(lines)-1].Stop)
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
