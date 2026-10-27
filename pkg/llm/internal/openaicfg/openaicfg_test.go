package openaicfg

import (
	"strings"
	"testing"
)

func TestSanitizeResponsesSchemaName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"점 포함 judge name", "twentyq_verify_guess.strict_identity_judge_01", "twentyq_verify_guess_strict_identity_judge_01"},
		{"이미 유효", "twentyq_answer_question", "twentyq_answer_question"},
		{"공백만", "   ", "schema"},
		{"빈 문자열", "", "schema"},
		{"여러 비허용 문자", "a.b/c d", "a_b_c_d"},
	}
	for _, c := range cases {
		if got := ResponsesSchemaName(c.in); got != c.want {
			t.Fatalf("%s: ResponsesSchemaName(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}

	if got := ResponsesSchemaName(strings.Repeat("a", 80)); len(got) != 64 {
		t.Fatalf("64자 cap 실패: len=%d", len(got))
	}
}

func TestValidateMaxRetriesRejectsNegative(t *testing.T) {
	if err := ValidateMaxRetries(-1); err == nil {
		t.Fatal("ValidateMaxRetries(-1) error = nil, want negative error")
	}

	for _, retries := range []int{0, 3} {
		if err := ValidateMaxRetries(retries); err != nil {
			t.Fatalf("ValidateMaxRetries(%d) error = %v", retries, err)
		}
	}
}
