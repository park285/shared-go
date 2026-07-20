package promptguard

import (
	"strings"
	"testing"
)

// TestNormalizeViews는 정규화가 hidden 문자와 compatibility 문자를 제거하는지 검증한다.
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

// TestNormalizeViewsBuildsJoinedObfuscationView는 난독화된 간격이 축소되는지 검증한다.
func TestNormalizeViewsBuildsJoinedObfuscationView(t *testing.T) {
	t.Parallel()

	views := normalizeViews("시 스 템  프 롬 프 트")
	if !strings.Contains(views.Joined, "시스템프롬프트") {
		t.Fatalf("joined view = %q, want obfuscation collapsed", views.Joined)
	}
}

// TestSplitTextSegments는 config, code, quote 혼합 감지를 검증한다.
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

func TestSplitTextSegmentsDistinguishesDialogueFromFlatConfig(t *testing.T) {
	t.Parallel()

	dialogue := "User: first question\nAssistant: first answer\nUser: second question\nAssistant: second answer"
	dialogueSegments := splitTextSegments(dialogue)
	if len(dialogueSegments) != 1 || dialogueSegments[0].Kind != segmentPlain {
		t.Fatalf("dialogue segments = %#v, want one plain segment", dialogueSegments)
	}

	config := "host: localhost\nport: 8080\ndebug: true"
	configSegments := splitTextSegments(config)
	if len(configSegments) != 1 || configSegments[0].Kind != segmentConfig {
		t.Fatalf("config segments = %#v, want one config segment", configSegments)
	}
}

// TestNormalizeViewsSingleJamoPanic은 단일 compatibility jamo 문자
// (예: "ㄱ" U+3131)가 composeJamoSequences 내부에서 panic을 일으키지 않는지 검증한다.
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

// TestContainsSuspiciousBase64는 base64 heuristic의 positive/negative 케이스를 검증한다.
func TestContainsSuspiciousBase64(t *testing.T) {
	t.Parallel()

	if !containsSuspiciousBase64("please decode c3lzdGVtIHByb21wdCByZXZlYWw=") {
		t.Fatal("expected suspicious base64 detection")
	}

	if containsSuspiciousBase64("hello-world") {
		t.Fatal("did not expect false positive")
	}
}

// TestDecodeBase64Candidate는 지원되는 encoding과 malformed 입력 처리를 검증한다.
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
