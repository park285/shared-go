package jsonutil

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/park285/shared-go/pkg/json"
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
	jsonEscape      byte = 92
)

// 코드펜스 정규식
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*([\\s\\S]*?)```")

// 1. 코드펜스 내 JSON 우선 시도
// 2. 브라켓 매칭으로 폴백
func Extract(text string) ([]byte, error) {
	return ExtractWithLimit(text, DefaultExtractMaxBytes)
}

func ExtractWithLimit(text string, maxBytes int) ([]byte, error) {
	if maxBytes > 0 && len(text) > maxBytes {
		return nil, ErrInputTooLarge
	}

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
	return ExtractToMapWithLimit(text, DefaultExtractMaxBytes)
}

func ExtractToMapWithLimit(text string, maxBytes int) (map[string]any, error) {
	if maxBytes > 0 && len(text) > maxBytes {
		return nil, ErrInputTooLarge
	}

	text = strings.TrimSpace(text)

	// 코드펜스 후보는 단일 Unmarshal로 valid 판정과 디코드를 겸한다(Valid+Unmarshal 이중 파싱 제거).
	// 기존 Extract 의미상 코드펜스가 valid JSON이면 거기서 확정하므로, valid지만 object가
	// 아니면 에러로 끝내고(폴백하지 않음), invalid일 때만 bracket 폴백으로 넘어간다.
	if matches := fenceRe.FindStringSubmatch(text); len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		switch firstJSONByte(candidate) {
		case jsonObjectOpen:
			var m map[string]any
			if err := json.Unmarshal([]byte(candidate), &m); err == nil {
				return m, nil
			}
		case 'n':
			if candidate == "null" {
				return nil, nil
			}
			if json.Valid([]byte(candidate)) {
				return nil, fmt.Errorf("unmarshal json: not a JSON object")
			}
		case jsonArrayOpen:
			if json.Valid([]byte(candidate)) {
				return nil, fmt.Errorf("unmarshal json: not a JSON object")
			}
		default:
			if json.Valid([]byte(candidate)) {
				return nil, fmt.Errorf("unmarshal json: not a JSON object")
			}
		}
	}

	data, err := extractFirstJSON(text)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	return result, nil
}

func firstJSONByte(s string) byte {
	for i := range len(s) {
		switch s[i] {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return s[i]
		}
	}
	return 0
}

// extractFirstJSON: 텍스트에서 첫 번째 유효한 JSON object/array를 추출합니다.
// 문자열 내 괄호와 이스케이프를 정확히 처리합니다.
func extractFirstJSON(text string) ([]byte, error) {
	b := []byte(text)
	// suffix 닫는 괄호 카운트: 시작점 뒤에 같은 타입 닫는 괄호가 0개면 findMatchingEnd가
	// 반드시 -1이므로 꼬리 재스캔 없이 건너뛴다. 닫힘 없는 괄호 홍수의 O(n²)을 선형화한다.
	objClose, arrClose := suffixCloseCounts(b)
	for i := range b {
		switch b[i] {
		case jsonObjectOpen:
			if objClose[i] == 0 {
				continue
			}
		case jsonArrayOpen:
			if arrClose[i] == 0 {
				continue
			}
		default:
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

func suffixCloseCounts(b []byte) (obj, arr []int) {
	n := len(b)
	obj = make([]int, n+1)
	arr = make([]int, n+1)
	for i := n - 1; i >= 0; i-- {
		obj[i] = obj[i+1]
		arr[i] = arr[i+1]
		switch b[i] {
		case jsonObjectClose:
			obj[i]++
		case jsonArrayClose:
			arr[i]++
		}
	}
	return obj, arr
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
