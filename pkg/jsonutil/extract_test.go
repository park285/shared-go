package jsonutil

import (
	"errors"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantJSON  string
		wantError error
	}{
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

	for _, tt := range tests {
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

func TestFindMatchingEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		start int
		want  int
	}{
		{
			name:  "단순 객체",
			input: `{"a": 1}`,
			start: 0,
			want:  7,
		},
		{
			name:  "중첩 객체",
			input: `{"outer": {"inner": 1}}`,
			start: 0,
			want:  22,
		},
		{
			name:  "문자열 내 괄호 무시",
			input: `{"msg": "test {value}"}`,
			start: 0,
			want:  22,
		},
		{
			name:  "이스케이프 쿼테이션",
			input: `{"quote": "He said \"hi\""}`,
			start: 0,
			want:  26,
		},
		{
			name:  "매칭 실패",
			input: `{"broken`,
			start: 0,
			want:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := findMatchingEnd([]byte(tt.input), tt.start)
			if result != tt.want {
				t.Errorf("findMatchingEnd() = %d, want %d", result, tt.want)
			}
		})
	}
}
