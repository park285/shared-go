package jsonutil

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrNoJSONFound = errors.New("no valid JSON found in response")

var ErrInputTooLarge = errors.New("jsonutil: input too large")

const DefaultExtractMaxBytes = 1 << 20

const (
	jsonObjectOpen  byte = 123
	jsonObjectClose byte = 125
	jsonArrayOpen   byte = 91
	jsonArrayClose  byte = 93
	jsonQuote       byte = 34
)

// 코드펜스 정규식.
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*([\\s\\S]*?)```")

// 1. 코드펜스 내 JSON 우선 시도
// 2. 브라켓 매칭으로 폴백.
func Extract(text string) ([]byte, error) {
	if len(text) > DefaultExtractMaxBytes {
		return nil, ErrInputTooLarge
	}

	text = strings.TrimSpace(text)

	// scalar(42, "s", null)는 object/array만 추출하던 기존 계약 보존을 위해 fast-path에서 의도적으로 제외.
	if len(text) > 0 && (text[0] == jsonObjectOpen || text[0] == jsonArrayOpen) {
		if whole := []byte(text); jsontext.Value(whole).IsValid() {
			return whole, nil
		}
	}

	// 1. 코드펜스 우선
	if matches := fenceRe.FindStringSubmatch(text); len(matches) > 1 {
		if candidate := []byte(strings.TrimSpace(matches[1])); jsontext.Value(candidate).IsValid() {
			return candidate, nil
		}
	}

	// 2. 브라켓 매칭 폴백
	out, err := extractFirstJSON(text)
	if err != nil {
		return out, fmt.Errorf("extract first JSON: %w", err)
	}

	return out, nil
}

// extractFirstJSON은 각 컨테이너를 뒤에서부터 한 번만 파싱한다. 부모는 이미 검증된
// 자식의 끝과 깊이를 재사용하므로 매칭뿐 아니라 중첩 후보의 검증도 반복하지 않는다.
// 모든 raw opener를 방문한다: 잘못된 외부 JSON의 문자열 안에도 유효한 후보가 있다.
// 후보의 문법상 직접 자식만 읽고 문자열은 토큰으로 건너뛴다. 문자열 안에서 시작한
// 후보도 별도로 보지만 문자열 중간의 escape를 바깥 문법으로 허용하지 않으므로
// 같은 긴 문자열 suffix를 opener마다 다시 토큰화하지 않는다.
func extractFirstJSON(text string) ([]byte, error) {
	start := strings.IndexAny(text, "{[")
	if start < 0 {
		return nil, ErrNoJSONFound
	}

	text = text[start:]

	p := extractionParser{
		data:  []byte(text),
		spans: make([]jsonSpan, len(text)),
	}

	first := -1

	for i := len(p.data) - 1; i >= 0; i-- {
		if p.data[i] != jsonObjectOpen && p.data[i] != jsonArrayOpen {
			continue
		}

		if span := p.container(i); span.end != 0 {
			p.spans[i] = span
			first = i
		}
	}

	if first < 0 {
		return nil, ErrNoJSONFound
	}

	return bytes.Clone(p.data[first:p.spans[first].end]), nil
}

// jsontext의 Go 1.27 기본 제한과 동일하다. 자식을 축약한 검증용 문서는 깊이를
// 잃으므로 원래 깊이를 별도로 전달한다. 경계 테스트는 표준 검증기와의 일치를 확인한다.
const extractMaxDepth = 10000

type jsonSpan struct {
	end   int
	depth int
}

type extractionParser struct {
	data    []byte
	spans   []jsonSpan
	scratch []byte
	reader  bytes.Buffer
	decoder *jsontext.Decoder
}

func (p *extractionParser) container(start int) jsonSpan {
	object := p.data[start] == jsonObjectOpen
	closeByte := jsonArrayClose

	if object {
		closeByte = jsonObjectClose
	}

	p.scratch = append(p.scratch[:0], p.data[start])

	pos, depth := p.space(start+1), 1

	if pos < len(p.data) && p.data[pos] == closeByte {
		return jsonSpan{end: pos + 1, depth: depth}
	}

	for pos < len(p.data) {
		if object {
			pos = p.memberName(pos)
			if pos < 0 {
				return jsonSpan{}
			}
		}

		end, childDepth := p.value(pos)
		if end == 0 || childDepth >= extractMaxDepth {
			return jsonSpan{}
		}

		depth = max(depth, childDepth+1)
		pos = p.space(end)

		if pos == len(p.data) {
			return jsonSpan{}
		}

		p.scratch = append(p.scratch, p.data[pos])
		if p.data[pos] == closeByte {
			// 자식은 0으로 축약되어 각 직접 멤버만 검증된다. 표준 검증기가
			// escape로 같은 이름이 된 key를 포함한 중복 이름 정책을 소유한다.
			if jsontext.Value(p.scratch).IsValid() {
				return jsonSpan{end: pos + 1, depth: depth}
			}

			return jsonSpan{}
		}

		if p.data[pos] != ',' {
			return jsonSpan{}
		}

		pos = p.space(pos + 1)
	}

	return jsonSpan{}
}

func (p *extractionParser) memberName(pos int) int {
	if pos >= len(p.data) || p.data[pos] != jsonQuote {
		return -1
	}

	end := p.scalar(pos)
	if end == 0 {
		return -1
	}

	pos = p.space(end)
	if pos == len(p.data) || p.data[pos] != ':' {
		return -1
	}

	p.scratch = append(p.scratch, ':')

	return p.space(pos + 1)
}

func (p *extractionParser) value(pos int) (end, depth int) {
	if pos >= len(p.data) {
		return 0, 0
	}

	if p.data[pos] == jsonObjectOpen || p.data[pos] == jsonArrayOpen {
		span := p.spans[pos]
		if span.end != 0 {
			p.scratch = append(p.scratch, '0')
		}

		return span.end, span.depth
	}

	return p.scalar(pos), 0
}

func (p *extractionParser) scalar(pos int) int {
	c := p.data[pos]
	if c != jsonQuote && c != '-' && (c < '0' || c > '9') && c != 't' && c != 'f' && c != 'n' {
		return 0
	}

	// bytes.Buffer는 decoder가 입력을 직접 읽게 하여 후보마다 suffix를 복사하지 않는다.
	p.reader = *bytes.NewBuffer(p.data[pos:])
	if p.decoder == nil {
		p.decoder = jsontext.NewDecoder(&p.reader)
	} else {
		p.decoder.Reset(&p.reader)
	}

	if _, err := p.decoder.ReadToken(); err != nil {
		return 0
	}

	end := pos + int(p.decoder.InputOffset())

	p.scratch = append(p.scratch, p.data[pos:end]...)

	return end
}

func (p *extractionParser) space(pos int) int {
	for pos < len(p.data) {
		switch p.data[pos] {
		case ' ', '\t', '\n', '\r':
			pos++
		default:
			return pos
		}
	}

	return pos
}
