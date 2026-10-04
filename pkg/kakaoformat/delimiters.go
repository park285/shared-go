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

// parseEmphasisDelimiter는 CommonMark 구분자 판정을 따르되 한국어 답변에서 흔한 세 경우만 조정한다.
// 문장부호로 끝난 강조 뒤에 조사가 붙은 `**50%**까지`는 닫히고, 숫자 사이 `3*4*5`의 곱셈 기호와
// `__init__.py`처럼 이름 안에서 끝나는 밑줄은 강조로 보지 않는다.
func parseEmphasisDelimiter(block text.Reader, minimum int, processor parser.DelimiterProcessor, pc parser.Context) *parser.Delimiter {
	before := block.PrecedingCharacter()
	line, _ := block.PeekLine()

	delimiter := parser.ParseDelimiter(block, minimum, processor, pc)
	if delimiter == nil {
		return nil
	}

	after, next := delimiterNeighbors(line, delimiter.OriginalLength)

	switch delimiter.Char {
	case '*':
		adjustStarDelimiter(delimiter, before, after)
	case '_':
		if delimiter.CanClose && (after == '.' || after == '(') && asciiAlphanumeric(next) {
			delimiter.CanClose = false
		}
	}

	return delimiter
}

// delimiterNeighbors는 구분자 묶음 바로 뒤 글자와 그다음 글자를 반환하며 줄 끝은 공백으로 본다.
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

	// CommonMark은 문장부호 뒤 구분자가 글자 앞에서 닫히지 못하게 하지만 한국어 조사는 공백 없이 붙는다.
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

// strikethroughParser는 `~~` 두 개짜리만 취소선으로 해석한다.
// GFM은 물결표 하나도 허용하지만 한국어의 `3~5일`·`10~20만 원` 범위 표기가 둘 이상이면 사이 문장이 지워진다.
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

// CloseBlock은 블록 간 상태를 보관하지 않는 취소선 파서의 종료 훅이다.
func (strikethroughParser) CloseBlock(ast.Node, parser.Context) {}
