package jsonutil

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/park285/shared-go/pkg/json"
)

var ErrNoJSONFound = errors.New("no valid JSON found in response")

const (
	jsonObjectOpen  byte = 123
	jsonObjectClose byte = 125
	jsonArrayOpen   byte = 91
	jsonArrayClose  byte = 93
	jsonQuote       byte = 34
	jsonEscape      byte = 92
)

// 코드펜스 정규식
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*([\\s\\S]*?)```")

// 1. 코드펜스 내 JSON 우선 시도
// 2. 브라켓 매칭으로 폴백
func Extract(text string) ([]byte, error) {
	text = strings.TrimSpace(text)

	// 1. 코드펜스 우선
	if matches := fenceRe.FindStringSubmatch(text); len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		if json.Valid([]byte(candidate)) {
			return []byte(candidate), nil
		}
	}

	// 2. 브라켓 매칭 폴백
	return extractFirstJSON(text)
}

func ExtractToMap(text string) (map[string]any, error) {
	data, err := Extract(text)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	return result, nil
}

// extractFirstJSON: 텍스트에서 첫 번째 유효한 JSON object/array를 추출합니다.
// 문자열 내 괄호와 이스케이프를 정확히 처리합니다.
func extractFirstJSON(text string) ([]byte, error) {
	b := []byte(text)
	for i := range b {
		if b[i] != jsonObjectOpen && b[i] != jsonArrayOpen {
			continue
		}
		end := findMatchingEnd(b, i)
		if end == -1 {
			continue
		}
		candidate := b[i : end+1]
		if json.Valid(candidate) {
			return candidate, nil
		}
	}
	return nil, ErrNoJSONFound
}

// findMatchingEnd: 문자열/이스케이프를 인식하여 매칭되는 닫는 괄호 위치를 반환합니다.
func findMatchingEnd(b []byte, start int) int {
	matcher := newJSONBracketMatcher(b[start])
	for i := start; i < len(b); i++ {
		if matcher.consume(b[i]) {
			return i
		}
	}
	return -1
}

type jsonBracketMatcher struct {
	open     byte
	close    byte
	depth    int
	inString bool
	escape   bool
}

func newJSONBracketMatcher(open byte) jsonBracketMatcher {
	closeBracket := jsonArrayClose
	if open == jsonObjectOpen {
		closeBracket = jsonObjectClose
	}
	return jsonBracketMatcher{open: open, close: closeBracket}
}

func (m *jsonBracketMatcher) consume(c byte) bool {
	if m.inString {
		m.consumeStringByte(c)
		return false
	}
	return m.consumeStructuralByte(c)
}

func (m *jsonBracketMatcher) consumeStringByte(c byte) {
	if m.escape {
		m.escape = false
		return
	}
	if c == jsonEscape {
		m.escape = true
		return
	}
	if c == jsonQuote {
		m.inString = false
	}
}

func (m *jsonBracketMatcher) consumeStructuralByte(c byte) bool {
	if c == jsonQuote {
		m.inString = true
		return false
	}
	if c == m.open {
		m.depth++
		return false
	}
	if c != m.close {
		return false
	}

	m.depth--
	return m.depth == 0
}
