package outputguard

import (
	"regexp"
	"slices"
	"strings"

	"github.com/park285/shared-go/pkg/internal/guardtext"
)

const maxOutputBytes = 1 << 20

type restrictedRule struct {
	id      string
	reason  ReasonCode
	pattern *regexp.Regexp
}

var restrictedRules = []restrictedRule{
	{id: "role_header_en", reason: ReasonRoleBlock, pattern: regexp.MustCompile(`(?im)^\s*(system|developer)\s*(prompt|message|instruction)s?\s*[:：]`)},
	{id: "role_header_ko", reason: ReasonRoleBlock, pattern: regexp.MustCompile(`(?im)^\s*(시스템|개발자|내부|숨겨진)\s*(프롬프트|메시지|지시|규칙|정책)\s*[:：]`)},
	{id: "role_tag", reason: ReasonRoleBlock, pattern: regexp.MustCompile(`(?i)<\s*/?\s*(system_prompt|system|developer|hidden_instructions|internal_policy)\s*>`)},
	{id: "role_paraphrase_en", reason: ReasonRoleBlock, pattern: regexp.MustCompile(`(?i)(hidden|internal|system|developer).{0,40}(instruction|prompt|policy|message).{0,40}(is|are|was|were|as follows|says)`)},
	{id: "role_paraphrase_ko", reason: ReasonRoleBlock, pattern: regexp.MustCompile(`(?i)(내부|숨겨진|시스템|개발자).{0,40}(지시|프롬프트|정책|규칙).{0,40}(다음|아래|내용|원문)`)},
	{id: "secret_assignment", reason: ReasonSecretPattern, pattern: regexp.MustCompile(`(?i)(api[_ -]?key|access[_ -]?token|refresh[_ -]?token|secret|password)\s*[:=]\s*[a-z0-9_\-]{8,}`)},
	{id: "private_key", reason: ReasonSecretPattern, pattern: regexp.MustCompile(`(?i)BEGIN [A-Z ]*PRIVATE KEY`)},
}

type Guard struct {
	cache *protectedIndexCache
}

func NewGuard() *Guard {
	return &Guard{cache: newProtectedIndexCache(defaultProtectedCacheCapacity, defaultProtectedCacheTTL, nil)}
}

func (g *Guard) Check(req CheckRequest) Evaluation {
	evaluation := Evaluation{Decision: DecisionAllow, OutputBytes: len(req.Text)}
	if len(req.Text) > maxOutputBytes {
		evaluation.Decision = DecisionBlock
		evaluation.ReasonCodes = []ReasonCode{ReasonOutputOversize}

		return evaluation
	}

	protectedTexts, ok := validateProtectedTexts(req.ProtectedTexts)
	if !ok {
		evaluation.Decision = DecisionBlock
		evaluation.ReasonCodes = []ReasonCode{ReasonProtectedInputOversize}

		return evaluation
	}

	surfaces := outputSurfaces(req.Text)
	collectRestrictedMatches(surfaces, &evaluation)
	if len(protectedTexts) > 0 && g.protectedOverlap(surfaces, protectedTexts) {
		appendReason(&evaluation, ReasonProtectedTextOverlap)
	}
	if len(evaluation.ReasonCodes) > 0 {
		evaluation.Decision = DecisionBlock
	}

	return evaluation
}

func (g *Guard) Validate(req CheckRequest) error {
	if g.Check(req).Decision == DecisionBlock {
		return ErrRestrictedGeneratedText
	}

	return nil
}

func (g *Guard) protectedOverlap(surfaces, protectedTexts []string) bool {
	if len(protectedTexts) == 0 || len(surfaces) == 0 {
		return false
	}
	var cache *protectedIndexCache
	if g != nil {
		cache = g.cache
	}
	if cache == nil {
		cache = newProtectedIndexCache(defaultProtectedCacheCapacity, defaultProtectedCacheTTL, nil)
	}
	index := cache.loadOrBuild(protectedTexts)
	for _, surface := range surfaces {
		if index.overlaps([]rune(guardtext.Normalize(surface))) {
			return true
		}
	}

	return false
}

func outputSurfaces(text string) []string {
	views := guardtext.NormalizeViews(text)
	stripped := guardtext.StripFormatAndCombining(text)
	decodedCandidates := guardtext.DecodeCandidates(text)
	surfaces := make([]string, 0, 4+len(decodedCandidates)*4)
	surfaces = append(surfaces, views.Raw, views.Norm, views.Joined, guardtext.Normalize(stripped))
	for _, decoded := range decodedCandidates {
		decodedViews := guardtext.NormalizeViews(decoded)
		decodedStripped := guardtext.StripFormatAndCombining(decoded)
		surfaces = append(surfaces, decodedViews.Raw, decodedViews.Norm, decodedViews.Joined, guardtext.Normalize(decodedStripped))
	}

	return compactStrings(surfaces)
}

func collectRestrictedMatches(surfaces []string, evaluation *Evaluation) {
	seenRules := make(map[string]struct{}, len(restrictedRules))
	for _, rule := range restrictedRules {
		for _, surface := range surfaces {
			if !rule.pattern.MatchString(surface) {
				continue
			}
			if _, exists := seenRules[rule.id]; !exists {
				evaluation.RuleIDs = append(evaluation.RuleIDs, rule.id)
				seenRules[rule.id] = struct{}{}
			}
			appendReason(evaluation, rule.reason)

			break
		}
	}
}

func appendReason(evaluation *Evaluation, reason ReasonCode) {
	if !slices.Contains(evaluation.ReasonCodes, reason) {
		evaluation.ReasonCodes = append(evaluation.ReasonCodes, reason)
	}
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}
