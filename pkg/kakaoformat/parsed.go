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

var plainParser = parser.New(
	parser.WithExtensions(extension.NewTableParser(), extension.NewTaskListItemParser(), extension.NewFootnoteParser()),
	parser.WithInlineParsers(
		util.Prioritized[parser.InlineParser](&literalURLParser{}, 150),
		util.Prioritized[parser.InlineParser](strikethroughParser{}, 500),
	),
	parser.WithParseDelimiterFunc(parseEmphasisDelimiter),
)

type plainDocument struct {
	source     []byte
	code       *store
	tableLines int
}

func render(input string) string {
	literal := newStore("NEUTRALIZED")
	code := newStore("CODE")
	protected, containers := protectCodeRanges(protectNeutralizedMarkers(input, literal), code)
	source := []byte(escapeDateListMarkers(protected, containers))
	root := plainParser.Parse(source)
	document := plainDocument{source: source, code: code}

	return literal.Restore(code.Restore(strings.TrimSpace(cleanupSpacing(document.blocks(root, 0)))))
}

func (d *plainDocument) blocks(parent ast.Node, level int) string {
	var (
		output   strings.Builder
		previous ast.Node
	)

	if parent.Kind() == ast.KindDocument {
		output.Grow(len(d.source))
	}

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
		return d.inline(node)
	case *ast.Heading:
		return d.inline(node)
	case *ast.ThematicBreak:
		return "---"
	case *ast.Blockquote:
		return d.prefixLines(d.blocks(node, level), "> ")
	case *ast.List:
		return d.list(n, level)
	case *extast.Table:
		return d.table(n)
	case *ast.CodeBlock:
		return d.codeBlock(n.Value.Str(d.source))
	case *ast.HTMLBlock:
		if n.HTMLBlockKind == ast.HTMLBlockKind2 {
			return ""
		}

		return strings.TrimSuffix(n.Value.Str(d.source), "\n")
	case *extast.FootnoteDefinition:
		return "[" + n.Label.Str(d.source) + "] " + d.blocks(node, level)
	default:
		return d.blocks(node, level)
	}
}

func (d *plainDocument) codeBlock(value string) string {
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	for index, line := range lines {
		lines[index] = d.code.Put(d.code.Restore(line))
	}

	return strings.Join(lines, "\n")
}

func (d *plainDocument) prefixLines(input, prefix string) string {
	d.code.indentFollowingLines(input, prefix)

	return prefixLines(input, prefix)
}

func prefixLines(input, prefix string) string {
	var output strings.Builder

	first := true

	for line := range strings.SplitSeq(input, "\n") {
		if !first {
			output.WriteByte('\n')
		}

		first = false

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

		marker := "- "

		if node.IsOrdered() {
			marker = strconv.Itoa(number) + ". "
			number++
		}

		first := item.FirstChild()
		if status, ok := extension.TaskStatusOf(item); ok {
			if status == extension.TaskStatusCompleted {
				marker += "[x] "
			} else {
				marker += "[ ] "
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
			if child == first && child.Kind() != ast.KindList {
				indent := listIndent(level) + strings.Repeat(" ", len(marker))
				d.code.indentFollowingLines(value, indent)

				value = strings.ReplaceAll(value, "\n", "\n"+indent)
			} else if child.Kind() != ast.KindList {
				value = d.prefixLines(value, listIndent(level+1))
			}

			output.WriteString(value)
		}
	}

	return output.String()
}

func (d *plainDocument) inline(parent ast.Node) string {
	var output strings.Builder

	d.inlines(&output, parent)

	return output.String()
}

func (d *plainDocument) inlines(output *strings.Builder, parent ast.Node) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		if end, value, ok := d.scriptTag(child); ok {
			output.WriteString(value)

			child = end

			continue
		}

		d.inlineNode(output, child)
	}
}

func (d *plainDocument) inlineNode(output *strings.Builder, node ast.Node) {
	switch n := node.(type) {
	case *ast.Text:
		output.WriteString(n.Value.Value(d.source))

		if n.SoftLineBreak() || n.HardLineBreak() {
			output.WriteByte('\n')
		}
	case *ast.Link:
		d.link(output, node, n.Destination.Value(d.source))
	case *ast.Image:
		d.link(output, node, n.Destination.Value(d.source))
	case *literalURL:
		output.WriteString(TransformEscapes(n.segment.Str(d.source), func(value string) string { return value }))
	case *ast.AutoLink:
		output.WriteString(n.Label.Value(d.source))
	case *ast.CodeSpan:
		output.WriteString(n.Value.Value(d.source))
	case *ast.RawHTML:
		output.WriteString(rawHTMLText(n.Value.Value(d.source)))
	case *extast.FootnoteReference:
		output.WriteString("[" + n.Label.Str(d.source) + "]")
	default:
		d.inlines(output, node)
	}
}

func (d *plainDocument) link(output *strings.Builder, node ast.Node, destination string) {
	label := d.inline(node)

	if label == "" || strings.EqualFold(label, destination) {
		output.WriteString(destination)
	} else if destination == "" {
		output.WriteString(label)
	} else {
		output.WriteString(label + "( " + destination + " )")
	}
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
		return source
	}

	for row := range tableRows(node) {
		start, end := row.Pos(), row.Pos()
		for end < len(d.source) && d.source[end] != '\n' {
			end++
		}

		if tableColumnCount(string(d.source[start:end])) > columns {
			return source
		}
	}

	d.tableLines += columns * (rows + 2)

	var output strings.Builder

	for row := range tableRows(node) {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}

		cells := make([]string, 0, columns)

		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, d.inline(cell))
		}

		output.WriteString(tableRow(cells))
	}

	return output.String()
}

func tableRow(cells []string) string {
	lines := make([][]string, len(cells))
	height := 0

	for column, cell := range cells {
		lines[column] = strings.Split(cell, "\n")
		height = max(height, len(lines[column]))
	}

	var output strings.Builder

	for line := range height {
		if line > 0 {
			output.WriteByte('\n')
		}

		for column, values := range lines {
			if column > 0 {
				output.WriteString(" | ")
			}

			if line < len(values) {
				if len(lines) == 1 && values[line] == "" {
					output.WriteByte('|')
				} else {
					output.WriteString(values[line])
				}
			}
		}
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
