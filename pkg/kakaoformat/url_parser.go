package kakaoformat

import (
	"bytes"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

var kindLiteralURL = ast.NewNodeKind("LiteralURL")

type literalURL struct {
	ast.BaseInline

	segment text.Segment
}

func (n *literalURL) Kind() ast.NodeKind { return kindLiteralURL }

// Dump는 Goldmark 진단에 원문 URL 노드의 종류를 제공한다.
func (n *literalURL) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, nil)
}

type literalURLParser struct{}

func (p *literalURLParser) Trigger() []byte { return []byte{':'} }

// CloseBlock은 블록 간 상태를 보관하지 않는 URL 파서의 종료 훅이다.
func (p *literalURLParser) CloseBlock(_ ast.Node, _ parser.Context) {}

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
	if prefix == 0 || !ok || previous.Value.Index().Stop != start || previous.Value.Index().Start > start-prefix {
		return nil
	}

	start -= prefix

	line := source[start:segment.Stop]
	match := reLiteralURL.FindIndex(line)

	if len(match) != 2 || match[0] != 0 {
		return nil
	}

	end := literalURLEnd(line, match[1], context.LastDelimiter())

	previous.Value = text.NewSingleLineValueFromIndex(text.NewIndex(previous.Value.Index().Start, start), reader.Decoder())
	if previous.Value.Index().Start == start {
		parent.RemoveChild(previous)
	}

	reader.Advance(end - prefix)

	node := &literalURL{segment: text.NewSegment(start, start+end)}
	node.Init(node)
	node.SetPos(start)

	return node
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

			closer := literalURLDelimiter(line, index, opener)

			if closer.CanClose && opener.Processor.CanOpenCloser(opener, closer) && opener.CalcConsumption(closer) > 0 {
				end = index
				break
			}
		}
	}

	return end
}

func literalURLDelimiter(line []byte, index int, opener *parser.Delimiter) *parser.Delimiter {
	before, _ := utf8.DecodeLastRune(line[:index])
	length := 1

	for index+length < len(line) && line[index+length] == opener.Char {
		length++
	}

	after := rune(' ')

	if index+length < len(line) {
		after, _ = utf8.DecodeRune(line[index+length:])
	}

	canOpen := parser.IsLeftFlankingDelimiterRun(before, after)
	canClose := parser.IsRightFlankingDelimiterRun(before, after)

	if opener.Char == '_' {
		canOpen, canClose = canOpen && (!canClose || util.IsPunctRune(before)), canClose && (!canOpen || util.IsPunctRune(after))
	}

	return parser.NewDelimiter(canOpen, canClose, length, opener.Char, opener.Processor)
}
