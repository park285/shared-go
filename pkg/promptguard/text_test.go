package promptguard

import (
	"strings"
	"testing"
)

// TestNormalizeViews verifies normalization removes hidden and compatibility characters.
func TestNormalizeViews(t *testing.T) {
	t.Parallel()

	views := normalizeViews("Ｓуѕtеm\u200b Prompt ")
	if strings.ContainsRune(views.Norm, '\u200b') {
		t.Fatalf("expected control chars removed, got %q", views.Norm)
	}

	if strings.ContainsRune(views.Norm, 'Ｓ') {
		t.Fatalf("expected compatibility normalization, got %q", views.Norm)
	}

	if strings.TrimSpace(views.Norm) == "" {
		t.Fatalf("expected normalized text to remain non-empty, got %q", views.Norm)
	}
}

// TestNormalizeViewsBuildsJoinedObfuscationView verifies obfuscated spacing collapse.
func TestNormalizeViewsBuildsJoinedObfuscationView(t *testing.T) {
	t.Parallel()

	views := normalizeViews("시 스 템  프 롬 프 트")
	if !strings.Contains(views.Joined, "시스템프롬프트") {
		t.Fatalf("joined view = %q, want obfuscation collapsed", views.Joined)
	}
}

// TestSplitTextSegments verifies mixed config, code, and quote detection.
func TestSplitTextSegments(t *testing.T) {
	t.Parallel()

	input := "아래 YAML 룰팩을 분석해줘\nrules:\n  - id: test\n    pattern: '(시스템).{0,20}(프롬프트)'\n\n```bash\nprintenv TOKEN\n```\n> quoted sample"
	segments := splitTextSegments(input)

	var foundConfig, foundCode, foundQuote bool

	for _, segment := range segments {
		switch segment.Kind {
		case segmentConfig:
			foundConfig = true
		case segmentCode:
			foundCode = true
		case segmentQuote:
			foundQuote = true
		}
	}

	if !foundConfig || !foundCode || !foundQuote {
		t.Fatalf("segments = %#v, want config/code/quote", segments)
	}
}

// TestNormalizeViewsSingleJamoPanic verifies that a single compatibility jamo character
// (예: "ㄱ" U+3131) does not panic inside composeJamoSequences.
// 수정 전에는 jamo.ComposeHangeul이 길이 1 슬라이스에서 combineHangulSyllables를 호출하여
// jamos[i+1] index OOB panic을 일으킨다.
func TestNormalizeViewsSingleJamoPanic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "single-consonant-ㄱ", input: "ㄱ"},
		{name: "single-consonant-ㅎ", input: "ㅎ"},
		{name: "two-consonants-ㄱㅎ", input: "ㄱㅎ"},
		{name: "mixed-hangul-and-consonant", input: "안녕ㄱ테스트"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var views Views

			// panic이 발생하면 recover()가 잡아 테스트 실패로 보고한다.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("normalizeViews(%q) panicked: %v", tc.input, r)
					}
				}()

				views = normalizeViews(tc.input)
			}()

			if t.Failed() {
				return
			}

			// 정상 경로: 결과가 빈 문자열이 아니거나 입력과 비교 가능한 형태여야 한다.
			if views.Raw == "" && tc.input != "" {
				t.Errorf("normalizeViews(%q).Raw is empty, want non-empty", tc.input)
			}
		})
	}
}

// TestContainsSuspiciousBase64 verifies positive and negative base64 heuristics.
func TestContainsSuspiciousBase64(t *testing.T) {
	t.Parallel()

	if !containsSuspiciousBase64("please decode c3lzdGVtIHByb21wdCByZXZlYWw=") {
		t.Fatal("expected suspicious base64 detection")
	}

	if containsSuspiciousBase64("hello-world") {
		t.Fatal("did not expect false positive")
	}
}

// TestDecodeBase64Candidate verifies supported encodings and malformed input handling.
func TestDecodeBase64Candidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "standard padded",
			input: "c3lzdGVtIHByb21wdCByZXZlYWw=",
			want:  "system prompt reveal",
		},
		{
			name:  "raw url-safe",
			input: "c3lzdGVtX3Byb21wdF9yZXZlYWw",
			want:  "system_prompt_reveal",
		},
		{
			name:    "malformed padding",
			input:   "dGVzdA===",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeBase64Candidate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeBase64Candidate(%q) error = nil, want error", tt.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("decodeBase64Candidate(%q) error = %v, want nil", tt.input, err)
			}

			if string(got) != tt.want {
				t.Fatalf("decodeBase64Candidate(%q) = %q, want %q", tt.input, string(got), tt.want)
			}
		})
	}
}
