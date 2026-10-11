package jsonutil

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

type extractCase struct {
	name      string
	input     string
	wantJSON  string
	wantError error
}

func extractFenceCases() []extractCase {
	return []extractCase{
		{
			name:     "코드펜스 내 JSON",
			input:    "```json\n{\"name\": \"test\"}\n```",
			wantJSON: `{"name": "test"}`,
		},
		{
			name:     "코드펜스 json 태그 없이",
			input:    "```\n{\"value\": 42}\n```",
			wantJSON: `{"value": 42}`,
		},
		{
			name:     "코드펜스 invalid JSON 뒤 유효한 JSON으로 폴백",
			input:    "```json\n{bad}\n```\nvalid payload: {\"ok\": true}",
			wantJSON: `{"ok": true}`,
		},
		{
			name:     "코드펜스 JSON을 브라켓 후보보다 우선",
			input:    "```json\n{\"source\": \"fence\"}\n```\n{\"source\": \"fallback\"}",
			wantJSON: `{"source": "fence"}`,
		},
		{
			name:     "마크다운 텍스트와 함께",
			input:    "Here is the result:\n```json\n{\"status\": \"ok\"}\n```\nDone!",
			wantJSON: `{"status": "ok"}`,
		},
	}
}

func extractBracketFallbackCases() []extractCase {
	return []extractCase{
		{
			name:     "브라켓 매칭 폴백 - Object",
			input:    "The answer is {\"foo\": \"bar\"} and more text",
			wantJSON: `{"foo": "bar"}`,
		},
		{
			name:     "브라켓 매칭 폴백 - Array",
			input:    "Here: [1, 2, 3] end",
			wantJSON: `[1, 2, 3]`,
		},
		{
			name:     "중첩된 객체",
			input:    `{"outer": {"inner": "value"}}`,
			wantJSON: `{"outer": {"inner": "value"}}`,
		},
		{
			name:     "문자열 내 괄호 처리",
			input:    `{"message": "Hello {world}"}`,
			wantJSON: `{"message": "Hello {world}"}`,
		},
		{
			name:     "문자열 내 닫는 괄호 처리",
			input:    `{"a":"}"}`,
			wantJSON: `{"a":"}"}`,
		},
		{
			name:     "이스케이프 처리",
			input:    `{"quote": "He said \"hi\""}`,
			wantJSON: `{"quote": "He said \"hi\""}`,
		},
		{
			name:     "이스케이프된 quote 처리",
			input:    `{"a":"b\"c"}`,
			wantJSON: `{"a":"b\"c"}`,
		},
		{
			name:     "다중 JSON object 중 첫 번째 valid만 추출",
			input:    `{"a":1}{"b":2}`,
			wantJSON: `{"a":1}`,
		},
		{
			name:     "깊은 중첩 JSON 추출",
			input:    `prefix {"a":{"b":[{"c":{"d":{"e":[1]}}}]}} suffix`,
			wantJSON: `{"a":{"b":[{"c":{"d":{"e":[1]}}}]}}`,
		},
	}
}

func extractNoJSONCases() []extractCase {
	return []extractCase{
		{
			name:      "비매칭 bracket",
			input:     `{"a":[1,2,3}`,
			wantError: ErrNoJSONFound,
		},
		{
			name:      "빈 입력",
			input:     "",
			wantError: ErrNoJSONFound,
		},
		{
			name:      "공백만 입력",
			input:     " \n\t ",
			wantError: ErrNoJSONFound,
		},
		{
			name:      "JSON 없음",
			input:     "No JSON here at all",
			wantError: ErrNoJSONFound,
		},
		{
			name:      "깨진 JSON",
			input:     "{broken",
			wantError: ErrNoJSONFound,
		},
	}
}

func TestExtract(t *testing.T) {
	t.Parallel()

	cases := slices.Concat(
		extractFenceCases(),
		extractBracketFallbackCases(),
		extractNoJSONCases(),
	)

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Extract(tt.input)

			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Errorf("Extract() error = %v, wantError %v", err, tt.wantError)
				}

				return
			}

			if err != nil {
				t.Fatalf("Extract() unexpected error: %v", err)
			}

			if string(result) != tt.wantJSON {
				t.Errorf("Extract() = %q, want %q", string(result), tt.wantJSON)
			}
		})
	}
}

func TestExtract_WholeDocumentFastPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantJSON  string
		wantError error
	}{
		{
			name:     "문자열 안의 코드펜스보다 전체 문서 우선",
			input:    "{\"note\":\"```json\\n{\\\"inner\\\":1}\\n```\"}",
			wantJSON: "{\"note\":\"```json\\n{\\\"inner\\\":1}\\n```\"}",
		},
		{
			name:     "선행/후행 공백은 트림 후 전체 반환",
			input:    "  \n{\"a\":1}\t ",
			wantJSON: `{"a":1}`,
		},
		{
			name:      "scalar 전체 문서는 fast-path 제외",
			input:     "42",
			wantError: ErrNoJSONFound,
		},
		{
			name:      "문자열 scalar 전체 문서는 fast-path 제외",
			input:     `"just a string"`,
			wantError: ErrNoJSONFound,
		},
		{
			name:      "null 전체 문서는 fast-path 제외",
			input:     "null",
			wantError: ErrNoJSONFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Extract(tt.input)

			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("Extract() error = %v, wantError %v", err, tt.wantError)
				}

				return
			}

			if err != nil {
				t.Fatalf("Extract() unexpected error: %v", err)
			}

			if string(got) != tt.wantJSON {
				t.Fatalf("Extract() = %q, want %q", string(got), tt.wantJSON)
			}
		})
	}
}

func TestExtract_ResultDoesNotPinInputBuffer(t *testing.T) {
	t.Parallel()

	payload := `{"ok":true}`
	input := strings.Repeat("prose ", 20000) + payload + strings.Repeat(" tail", 20000)

	got, err := Extract(input)
	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}

	if string(got) != payload {
		t.Fatalf("Extract() = %q, want %q", string(got), payload)
	}

	if cap(got) > 1024 {
		t.Fatalf("Extract() result cap = %d, want <= 1024 (입력 전체 버퍼를 alias하면 안 됨, 입력 %d바이트)", cap(got), len(input))
	}
}

func TestExtract_BracketSemantics(t *testing.T) {
	t.Parallel()

	tests := []extractCase{
		{name: "invalid outer valid inner", input: `{"bad": [1,], "ok": {"v":2}}`, wantJSON: `{"v":2}`},
		{name: "unclosed outer valid inner", input: `{"outer": {"ok":true}`, wantJSON: `{"ok":true}`},
		{name: "mixed mismatched outer", input: `{[} {"ok":true}]`, wantJSON: `{"ok":true}`},
		{name: "quoted prose opener", input: `"prose [1,2] text"`, wantJSON: `[1,2]`},
		{name: "candidate in invalid object's string", input: `{"text":"[1,2]",bad}`, wantJSON: `[1,2]`},
		{name: "candidate crosses quote context", input: `{"text":"[","]" broken}`, wantJSON: `[","]`},
		{name: "escaped quoted candidate", input: `{"text":"{\"ok\":1}",bad}`, wantError: ErrNoJSONFound},
		{name: "escaped backslash before quote", input: `prefix {"s":"\\","v":[1]} tail`, wantJSON: `{"s":"\\","v":[1]}`},
		{name: "mismatched brackets in string", input: `prefix {"s":"][}{","v":[]} tail`, wantJSON: `{"s":"][}{","v":[]}`},
		{name: "duplicate escaped key", input: `{"a":[],"\u0061":1}`, wantJSON: `[]`},
		{name: "invalid number", input: `[01,{"ok":true}]`, wantJSON: `{"ok":true}`},
		{name: "invalid UTF8", input: "[\"\xff\",{}]", wantJSON: `{}`},
		{name: "invalid surrogate", input: `["\ud800",{}]`, wantJSON: `{}`},
		{name: "only first fence prioritized", input: "{} ```json\ninvalid\n``` ```json\n[2]\n```", wantJSON: `{}`},
		{name: "fence before earlier raw candidate", input: "{} ```json\n[2]\n```", wantJSON: `[2]`},
		{name: "fenced scalar", input: "[] ```json\n42\n```", wantJSON: `42`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Extract(tt.input)
			if !errors.Is(err, tt.wantError) || string(got) != tt.wantJSON {
				t.Fatalf("Extract(%q) = %q, %v; want %q, %v", tt.input, got, err, tt.wantJSON, tt.wantError)
			}
		})
	}
}
