package kakaoformat

import (
	"bytes"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var kindLiteralURL = ast.NewNodeKind("LiteralURL")

type literalURL struct {
	ast.BaseInline

	segment text.Segment
}

func (n *literalURL) Kind() ast.NodeKind            { return kindLiteralURL }
func (n *literalURL) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type literalURLParser struct{}

func (p *literalURLParser) Trigger() []byte { return []byte{':'} }

// URL 내부 별표·밑줄은 기존 일반톡 계약에 따라 강조 대신 주소의 일부로 보존한다.
func (p *literalURLParser) Parse(parent ast.Node, reader text.Reader, context parser.Context) ast.Node {
	_, segment := reader.PeekLine()
	source := reader.Source()
	start := segment.Start
	prefix := 0

	if start >= 5 && bytes.Equal(source[start-5:start], []byte("https")) {
		prefix = 5
	} else if start >= 4 && bytes.Equal(source[start-4:start], []byte("http")) {
		prefix = 4
	}

	previous, ok := parent.LastChild().(*ast.Text)
	if prefix == 0 || !ok || previous.Segment.Stop != start || previous.Segment.Start > start-prefix {
		return nil
	}

	start -= prefix

	line := source[start:segment.Stop]
	match := reLiteralURL.FindIndex(line)

	if len(match) != 2 || match[0] != 0 {
		return nil
	}

	end := literalURLEnd(line, match[1], context.LastDelimiter())

	previous.Segment = previous.Segment.WithStop(start)
	if previous.Segment.Start == start {
		parent.RemoveChild(parent, previous)
	}

	reader.Advance(end - prefix)

	return &literalURL{segment: text.NewSegment(start, start+end)}
}

func literalURLEnd(line []byte, end int, last *parser.Delimiter) int {
	// 주소 바깥에서 열린 강조의 닫힘 판정은 Goldmark의 동일한 구분자 규칙을 사용한다.
	if !bytes.ContainsAny(line[:end], "*_~") {
		return end
	}

	for opener := last; opener != nil; opener = opener.PreviousDelimiter {
		if !opener.CanOpen {
			continue
		}

		for index := 0; index < end; index++ {
			if line[index] != opener.Char || markdownEscaped(string(line), index) {
				continue
			}

			before, _ := utf8.DecodeLastRune(line[:index])
			closer := parser.ScanDelimiter(line[index:], before, 1, opener.Processor)

			if closer != nil && closer.CanClose && opener.Processor.CanOpenCloser(opener, closer) && opener.CalcComsumption(closer) > 0 {
				end = index
				break
			}
		}
	}

	return end
}
