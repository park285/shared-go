package kakaoformat

import (
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

func parseEmphasisDelimiter(block text.Reader, minimum int, processor parser.DelimiterProcessor, pc parser.Context) ast.Node {
	before := block.PrecedingCharacter()
	line, segment := block.PeekLine()

	if len(line) == 0 || !processor.IsDelimiter(line[0]) {
		return nil
	}

	char := line[0]

	length := 0
	for length < len(line) && line[length] == char {
		length++
	}

	if length < minimum {
		return nil
	}

	after, next := delimiterNeighbors(line, length)
	canOpen, canClose := commonMarkFlanking(char, before, after)
	delimiter := parser.NewDelimiter(canOpen, canClose, length, char, processor)

	switch char {
	case '*':
		adjustStarDelimiter(delimiter, before, after)
	case '_':
		if delimiter.CanClose && (after == '.' || after == '(') && asciiAlphanumeric(next) {
			delimiter.CanClose = false
		}
	}

	delimiter.Value = text.NewSingleLineValueFromSegment(segment.WithStop(segment.Start+length), block.Decoder())
	block.Advance(length)
	pc.PushDelimiter(delimiter)

	return delimiter
}

func commonMarkFlanking(char byte, before, after rune) (canOpen, canClose bool) {
	left := parser.IsLeftFlankingDelimiterRun(before, after)
	right := parser.IsRightFlankingDelimiterRun(before, after)

	if char != '_' {
		return left, right
	}

	return left && (!right || util.IsPunctRune(before)), right && (!left || util.IsPunctRune(after))
}

func delimiterNeighbors(line []byte, length int) (rune, rune) {
	if length >= len(line) {
		return ' ', ' '
	}

	after, size := utf8.DecodeRune(line[length:])
	next := rune(' ')

	if rest := line[length+size:]; len(rest) > 0 {
		next, _ = utf8.DecodeRune(rest)
	}

	return after, next
}

func adjustStarDelimiter(delimiter *parser.Delimiter, before, after rune) {
	if asciiDigit(before) && asciiDigit(after) {
		delimiter.CanOpen, delimiter.CanClose = false, false

		return
	}

	if !delimiter.CanClose && !util.IsSpaceRune(before) && util.IsPunctRune(before) && eastAsianLetter(after) {
		delimiter.CanClose = true
	}

	if !delimiter.CanOpen && !util.IsSpaceRune(after) && util.IsPunctRune(after) && eastAsianLetter(before) {
		delimiter.CanOpen = true
	}
}

func asciiDigit(char rune) bool { return char >= '0' && char <= '9' }

func asciiAlphanumeric(char rune) bool {
	return asciiDigit(char) || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func eastAsianLetter(char rune) bool {
	return unicode.In(char, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana)
}

type tildeDelimiterProcessor struct{}

func (tildeDelimiterProcessor) IsDelimiter(char byte) bool { return char == '~' }

func (tildeDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (tildeDelimiterProcessor) OnMatch(int) ast.Node { return extast.NewStrikethrough() }

type strikethroughParser struct{}

func (strikethroughParser) Trigger() []byte { return []byte{'~'} }

func (strikethroughParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	if block.PrecedingCharacter() == '~' {
		return nil
	}

	line, _ := block.PeekLine()

	length := 0
	for length < len(line) && line[length] == '~' {
		length++
	}

	if length != 2 {
		return nil
	}

	return parser.ParseDelimiter(block, 2, tildeDelimiterProcessor{}, pc)
}

func (strikethroughParser) CloseBlock(ast.Node, parser.Context) {}
