package kakaoformat

import "strings"

// CodeRange는 Markdown 코드 구간의 바이트 경계와 fenced block 표시 정보를 보관한다.
// End와 BodyEnd는 구간 다음 위치이며 원문은 변경하지 않는다.
type CodeRange struct {
	Start, End, BodyStart, BodyEnd int
	Indent, Language               string
	Width                          int
	Block, Fenced, Container       bool
}

// CodeRanges는 코드 블록과 code span을 겹치지 않는 원문 순서로 반환한다.
// 인용·목록 안 fence, 닫히지 않은 fence와 여러 줄 code span도 코드로 취급한다.
func CodeRanges(input string) []CodeRange {
	blocks := codeBlocks(input)

	var result []CodeRange

	start := 0

	for _, block := range blocks {
		result = append(result, inlineCodeRanges(input[start:block.Start], start)...)
		result = append(result, block)
		start = block.End
	}

	return append(result, inlineCodeRanges(input[start:], start)...)
}

func codeBlocks(input string) []CodeRange {
	var (
		result []CodeRange
		lists  listContext
	)

	previousBlank := true

	for offset := 0; offset < len(input); {
		lineEnd := codeLineEnd(input, offset)
		base := lists.observe(input[offset:lineEnd], previousBlank)
		block, ok := codeBlockAt(input, offset, lineEnd, previousBlank, base)

		previousBlank = strings.TrimSpace(input[offset:lineEnd]) == ""

		if ok {
			result = append(result, block)
			offset = block.End
		} else {
			offset = lineEnd
		}
	}

	return result
}

// codeBlockAt은 base가 0보다 크면 그 목록 항목 내용 들여쓰기를 기준으로 코드 블록을 판정한다.
func codeBlockAt(input string, offset, lineEnd int, previousBlank bool, base int) (CodeRange, bool) {
	content, quotes := stripCodeQuotes(input[offset:lineEnd])
	indent := len(content) - len(strings.TrimLeft(content, " "))
	trimmed := content[indent:]
	listWidth := codeListPrefix(trimmed)

	if listWidth > 0 {
		trimmed = strings.TrimLeft(trimmed[listWidth:], " ")
	}

	if quotes > 0 || indent < base {
		base = 0
	}

	relative := indent - base

	if previousBlank && quotes == 0 && listWidth == 0 && (relative >= 4 || base == 0 && strings.HasPrefix(content, "\t")) {
		end := indentedCodeEnd(input, lineEnd, base)
		return CodeRange{Start: offset, End: end, BodyStart: offset, BodyEnd: end, Block: true}, true
	}

	width := codeFenceWidth(trimmed)
	if width < 3 || relative > 3 || trimmed[0] == '`' && strings.Contains(trimmed[width:], "`") {
		return CodeRange{}, false
	}

	block := CodeRange{Start: offset, End: len(input), BodyStart: lineEnd, BodyEnd: len(input), Indent: content[:indent], Language: strings.TrimSpace(trimmed[width:]), Width: width, Block: true, Fenced: true, Container: quotes > 0 || listWidth > 0}
	listIndent := base

	if listWidth > 0 {
		listIndent = indent + listWidth
	}

	return closeCodeBlock(input, block, quotes, listIndent, trimmed[0]), true
}

func indentedCodeEnd(input string, start, base int) int {
	prefix := strings.Repeat(" ", base+4)

	end := start
	for end < len(input) {
		next := codeLineEnd(input, end)
		part := input[end:next]

		if strings.TrimSpace(part) != "" && !strings.HasPrefix(part, prefix) && (base > 0 || !strings.HasPrefix(part, "\t")) {
			break
		}

		end = next
	}

	return end
}

func closeCodeBlock(input string, block CodeRange, quotes, listIndent int, marker byte) CodeRange {
	for end := block.BodyStart; end < len(input); {
		next := codeLineEnd(input, end)
		part, currentQuotes := stripCodeQuotes(input[end:next])
		leading := len(part) - len(strings.TrimLeft(part, " "))

		if currentQuotes < quotes || listIndent > 0 && strings.TrimSpace(part) != "" && leading < listIndent {
			block.End, block.BodyEnd = end, end
			break
		}

		if listIndent > 0 && leading >= listIndent {
			part = part[listIndent:]

			leading -= listIndent
		}

		part = strings.TrimLeft(part, " ")

		closing := codeFenceWidth(part)

		if leading <= 3 && closing >= block.Width && part[0] == marker && strings.TrimSpace(part[closing:]) == "" {
			block.End, block.BodyEnd = next, end
			break
		}

		end = next
	}

	return block
}

func stripCodeQuotes(line string) (string, int) {
	depth := 0

	for {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
			return line, depth
		}

		line = strings.TrimPrefix(trimmed[1:], " ")
		depth++
	}
}

func codeListPrefix(line string) int {
	i := 0

	if len(line) > 1 && strings.ContainsRune("-*+", rune(line[0])) {
		i = 1
	} else {
		for i < len(line) && i < 9 && line[i] >= '0' && line[i] <= '9' {
			i++
		}

		if i == 0 || i >= len(line) || line[i] != '.' && line[i] != ')' {
			return 0
		}

		i++
	}

	if i >= len(line) || line[i] != ' ' {
		return 0
	}

	return i + 1
}

func codeLineEnd(input string, start int) int {
	if end := strings.IndexByte(input[start:], '\n'); end >= 0 {
		return start + end + 1
	}

	return len(input)
}

func codeFenceWidth(line string) int {
	if len(line) == 0 || line[0] != '`' && line[0] != '~' {
		return 0
	}

	width := 0
	for width < len(line) && line[width] == line[0] {
		width++
	}

	return width
}

func markdownEscaped(input string, index int) bool {
	count := 0

	for index--; index >= 0 && input[index] == '\\'; index-- {
		count++
	}

	return count%2 != 0
}

// listContext는 목록 항목 내용의 들여쓰기를 추적한다. 항목 안에서 빈 줄 뒤 4칸 들여쓴 문단과 fence는
// CommonMark에서 항목 내용이므로, 문서 최상위 기준으로 들여쓰기 코드로 오인하지 않게 한다.
type listContext struct{ indents []int }

// observe는 줄이 속한 목록 항목의 내용 들여쓰기를 반환하고, 그 줄이 목록 항목이면 다음 줄을 위해 기록한다.
func (c *listContext) observe(line string, previousBlank bool) int {
	if strings.TrimSpace(line) == "" {
		return 0
	}

	content, quotes := stripCodeQuotes(line)
	if quotes > 0 {
		// 인용 안 목록은 추적하지 않으며 인용 시작은 바깥 목록을 끝낸다.
		c.indents = c.indents[:0]

		return 0
	}

	indent := len(content) - len(strings.TrimLeft(content, " "))
	trimmed := content[indent:]
	marker := codeListPrefix(trimmed)

	for len(c.indents) > 0 && indent < c.indents[len(c.indents)-1] {
		// 빈 줄 없이 덜 들여쓴 일반 줄은 앞 문단의 lazy continuation이므로 항목을 닫지 않는다.
		if !previousBlank && marker == 0 && codeFenceWidth(trimmed) < 3 {
			return 0
		}

		c.indents = c.indents[:len(c.indents)-1]
	}

	base := 0

	if len(c.indents) > 0 {
		base = c.indents[len(c.indents)-1]
	}

	if marker > 0 {
		c.indents = append(c.indents, indent+listContentOffset(trimmed, marker))
	}

	return base
}

// listContentOffset은 목록 표식 뒤 공백까지 포함한 항목 내용의 시작 열을 CommonMark 규칙대로 계산한다.
func listContentOffset(line string, marker int) int {
	spaces := 0
	for marker-1+spaces < len(line) && line[marker-1+spaces] == ' ' {
		spaces++
	}

	if spaces > 4 {
		spaces = 1
	}

	return marker - 1 + spaces
}
