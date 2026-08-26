package stringutil

import (
	"strings"
	"testing"
)

var truncateStringSink string

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{
			name:     "짧은 문자열",
			input:    "Hello",
			maxRunes: 10,
			want:     "Hello",
		},
		{
			name:     "정확히 길이 맞음",
			input:    "Hello",
			maxRunes: 5,
			want:     "Hello",
		},
		{
			name:     "영어 자르기",
			input:    "Hello World",
			maxRunes: 5,
			want:     "Hello...",
		},
		{
			name:     "한글 자르기 (Rune 단위)",
			input:    "안녕하세요",
			maxRunes: 3,
			want:     "안녕하...",
		},
		{
			name:     "혼합 문자열",
			input:    "Hello 안녕",
			maxRunes: 7,
			want:     "Hello 안...",
		},
		{
			name:     "이모지 포함",
			input:    "Hello 😀 World",
			maxRunes: 7,
			want:     "Hello 😀...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxRunes)
			if result != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxRunes, result, tt.want)
			}
		})
	}
}

func TestTruncateStringPanicsForNegativeLimit(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("TruncateString() did not panic for a negative limit")
		}
	}()

	_ = TruncateString("value", -1)
}

func TestTruncateStringPreservesInvalidUTF8Replacement(t *testing.T) {
	t.Parallel()

	input := string([]byte{'a', 0xff, 'b'})
	if got, want := TruncateString(input, 2), "a\ufffd..."; got != want {
		t.Fatalf("TruncateString() = %q, want %q", got, want)
	}
}

func TestTruncateStringNoTruncationAllocations(t *testing.T) {
	input := strings.Repeat("가", 16*1024)
	allocations := testing.AllocsPerRun(100, func() {
		truncateStringSink = TruncateString(input, 32*1024)
	})

	if allocations != 0 {
		t.Fatalf("TruncateString() allocations = %v, want 0", allocations)
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{
			name:  "포함됨",
			slice: []string{"apple", "banana", "cherry"},
			item:  "banana",
			want:  true,
		},
		{
			name:  "포함 안됨",
			slice: []string{"apple", "banana", "cherry"},
			item:  "grape",
			want:  false,
		},
		{
			name:  "빈 슬라이스",
			slice: []string{},
			item:  "test",
			want:  false,
		},
		{
			name:  "대소문자 구분",
			slice: []string{"Apple", "Banana"},
			item:  "apple",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsString(tt.slice, tt.item)
			if result != tt.want {
				t.Errorf("ContainsString(%v, %q) = %v, want %v", tt.slice, tt.item, result, tt.want)
			}
		})
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "앞뒤 공백",
			input: "  hello  ",
			want:  testHello,
		},
		{
			name:  "탭 포함",
			input: "\thello\t",
			want:  testHello,
		},
		{
			name:  "개행 포함",
			input: "\nhello\n",
			want:  testHello,
		},
		{
			name:  "공백 없음",
			input: testHello,
			want:  testHello,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimSpace(tt.input)
			if result != tt.want {
				t.Errorf("TrimSpace(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "대문자 변환",
			input: "HELLO",
			want:  testHello,
		},
		{
			name:  "공백 제거",
			input: "  Hello  ",
			want:  testHello,
		},
		{
			name:  "혼합",
			input: "  HELLO World  ",
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Normalize(tt.input)
			if result != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "빈 문자열",
			input: "",
			want:  "",
		},
		{
			name:  "공백 제거",
			input: "Hello World",
			want:  testHelloworld,
		},
		{
			name:  "하이픈 제거",
			input: testHelloWorld,
			want:  testHelloworld,
		},
		{
			name:  "언더스코어 제거",
			input: "hello_world",
			want:  testHelloworld,
		},
		{
			name:  "점 제거",
			input: "hello.world",
			want:  testHelloworld,
		},
		{
			name:  "느낌표 제거",
			input: "hello!world",
			want:  testHelloworld,
		},
		{
			name:  "특수문자 혼합",
			input: "  Hello-World_Test.Example!  ",
			want:  "helloworldtestexample",
		},
		{
			name:  "Unicode 별표",
			input: "test☆example",
			want:  testTestexample,
		},
		{
			name:  "Unicode 중점",
			input: "test・example",
			want:  testTestexample,
		},
		{
			name:  "Unicode 따옴표",
			input: "test'example'",
			want:  testTestexample,
		},
		{
			name:  "일본어 장음",
			input: "testーexample",
			want:  testTestexample,
		},
		{
			name:  "em dash",
			input: "test—example",
			want:  testTestexample,
		},
		{
			name:  "한글 보존",
			input: "안녕-세상",
			want:  "안녕세상",
		},
		{
			name:  "일본어 보존",
			input: "こんにちは・世界",
			want:  "こんにちは世界",
		},
		{
			name:  "혼합 언어",
			input: "  Test-テスト_시험!  ",
			want:  "testテスト시험",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeKey(tt.input)
			if result != tt.want {
				t.Errorf("NormalizeKey(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "빈 문자열",
			input: "",
			want:  "",
		},
		{
			name:  "공백을 하이픈으로",
			input: "hello world",
			want:  testHelloWorld,
		},
		{
			name:  "대문자 소문자 변환",
			input: "Hello World",
			want:  testHelloWorld,
		},
		{
			name:  "따옴표 제거",
			input: "it's great",
			want:  "its-great",
		},
		{
			name:  "점 제거",
			input: "hello.world",
			want:  testHelloworld,
		},
		{
			name:  "느낌표 제거",
			input: "hello!world",
			want:  testHelloworld,
		},
		{
			name:  "복합 특수문자",
			input: "Hello World!",
			want:  testHelloWorld,
		},
		{
			name:  "여러 공백",
			input: "hello  world  test",
			want:  "hello--world--test",
		},
		{
			name:  "앞뒤 공백 제거",
			input: "  hello world  ",
			want:  testHelloWorld,
		},
		{
			name:  "한글 보존",
			input: "안녕 세상",
			want:  "안녕-세상",
		},
		{
			name:  "일본어 보존",
			input: "こんにちは 世界",
			want:  "こんにちは-世界",
		},
		{
			name:  "혼합 언어 URL",
			input: "Test Page 테스트",
			want:  "test-page-테스트",
		},
		{
			name:  "특수문자 혼합",
			input: "It's a test. Great!",
			want:  "its-a-test-great",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Slugify(tt.input)
			if result != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}
