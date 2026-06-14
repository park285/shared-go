package outputguard

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var ErrRestrictedGeneratedText = errors.New("generated answer contains restricted output")

var restrictedGeneratedTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^\s*(system|developer)\s*(prompt|message|instruction)s?\s*[:：]`),
	regexp.MustCompile(`(?im)^\s*(시스템|개발자|내부|숨겨진)\s*(프롬프트|메시지|지시|규칙|정책)\s*[:：]`),
	regexp.MustCompile(`(?i)<\s*/?\s*(system_prompt|system|developer|hidden_instructions|internal_policy)\s*>`),
	regexp.MustCompile(`(?i)(hidden|internal|system|developer).{0,40}(instruction|prompt|policy|message).{0,40}(is|are|was|were|as follows|says)`),
	regexp.MustCompile(`(?i)(내부|숨겨진|시스템|개발자).{0,40}(지시|프롬프트|정책|규칙).{0,40}(다음|아래|내용|원문)`),
	regexp.MustCompile(`(?i)(api[_ -]?key|access[_ -]?token|refresh[_ -]?token|secret|password)\s*[:=]\s*[a-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)BEGIN [A-Z ]*PRIVATE KEY`),
}

var confusableLatinReplacer = strings.NewReplacer(
	"а", "a",
	"е", "e",
	"о", "o",
	"р", "p",
	"с", "c",
	"у", "y",
	"х", "x",
	"ѕ", "s",
	"і", "i",
	"ј", "j",
	"һ", "h",
	"ԁ", "d",
	"ԛ", "q",
	"ԝ", "w",
	"А", "a",
	"В", "b",
	"Е", "e",
	"К", "k",
	"М", "m",
	"Н", "h",
	"О", "o",
	"Р", "p",
	"С", "c",
	"Т", "t",
	"Х", "x",
)

func ValidateGeneratedText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	if matchesRestrictedPattern(trimmed) {
		return ErrRestrictedGeneratedText
	}

	normalized := normalizeForRestrictionMatch(trimmed)
	if normalized != trimmed && matchesRestrictedPattern(normalized) {
		return ErrRestrictedGeneratedText
	}

	return nil
}

func normalizeForRestrictionMatch(text string) string {
	stripped := stripFormatAndCombining(text)
	nfkc := norm.NFKC.String(stripped)
	deconfused := confusableLatinReplacer.Replace(nfkc)

	return strings.ToLower(deconfused)
}

func stripFormatAndCombining(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) {
			return -1
		}

		return r
	}, text)
}

func matchesRestrictedPattern(text string) bool {
	for _, pattern := range restrictedGeneratedTextPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}

	return false
}
