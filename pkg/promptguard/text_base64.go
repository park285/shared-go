package promptguard

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/park285/shared-go/pkg/internal/guardtext"
)

const (
	maxBase64Candidates         = 8
	maxDecodedRuleFragmentBytes = 14
)

func decodedTextSegments(input string) ([]textSegment, guardtext.DecodeStatus) {
	return textSegmentsFromDecodeResult(guardtext.DecodeCandidatesWithContext(input))
}

func (g *Guard) decodedTextSegments(input string) ([]textSegment, guardtext.DecodeStatus) {
	if g == nil {
		return decodedTextSegments(input)
	}

	result := guardtext.DecodeCandidatesWithContextForRuleOwner(input, g, decodedCandidateMayContributeForGuard)
	return textSegmentsFromDecodeResult(result)
}

func decodedCandidateMayContributeForGuard(guard *Guard, candidate string) bool {
	return guard.decodedCandidateMayContribute(candidate)
}

func textSegmentsFromDecodeResult(result guardtext.DecodeResult) ([]textSegment, guardtext.DecodeStatus) {
	segments := make([]textSegment, 0, min(len(result.Candidates), maxBase64Candidates))
	for _, candidate := range result.Candidates {
		if len(segments) >= maxBase64Candidates {
			break
		}
		segments = append(segments, textSegment{Kind: segmentPlain, Views: normalizeViews(candidate)})
	}

	return segments, result.Status
}

func (g *Guard) decodedCandidateMayContribute(candidate string) bool {
	if !guardtext.DecodedCandidateFitsBudget(candidate) {
		return false
	}
	views := normalizeViews(candidate)
	if decodedCandidateHasBoundarySyntax(views.Raw) {
		return true
	}
	shortFragment := len(candidate) <= maxDecodedRuleFragmentBytes
	for i := range g.packs {
		for j := range g.packs[i].Rules {
			rule := &g.packs[i].Rules[j]
			text := decodedContributionView(rule, views)
			if shortFragment && decodedTextOverlapsRequiredLiterals(text, rule.RequiredLiteralGroups) {
				return true
			}
			if !shortFragment && len(rule.RequiredLiteralGroups) > 0 &&
				containsAllLiteralGroups(text, rule.RequiredLiteralGroups) {
				return true
			}
		}
	}

	return false
}

func decodedContributionView(rule *compiledRule, views Views) string {
	switch rule.View {
	case viewJoined, viewAggregateJoined:
		return views.Joined
	default:
		return views.Norm
	}
}

func decodedTextOverlapsRequiredLiterals(text string, groups [][]string) bool {
	if text == "" {
		return false
	}
	for _, group := range groups {
		for _, literal := range group {
			if decodedTextOverlapsLiteral(text, literal) {
				return true
			}
		}
	}
	return false
}

func decodedTextOverlapsLiteral(text, literal string) bool {
	if literal == "" {
		return false
	}
	if strings.Contains(text, literal) {
		return true
	}

	start := -1
	for index, value := range text {
		if unicode.IsLetter(value) || unicode.IsNumber(value) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 && decodedRunOverlapsLiteral(text[start:index], literal) {
			return true
		}
		start = -1
	}
	return start >= 0 && decodedRunOverlapsLiteral(text[start:], literal)
}

func decodedRunOverlapsLiteral(run, literal string) bool {
	minimum := 3
	if containsNonASCII(run) {
		minimum = 2
	}
	if utf8.RuneCountInString(run) < minimum {
		return false
	}
	return strings.HasPrefix(literal, run) || strings.HasSuffix(literal, run)
}

func containsNonASCII(value string) bool {
	for _, r := range value {
		if r >= utf8.RuneSelf {
			return true
		}
	}
	return false
}

func decodedCandidateHasBoundarySyntax(value string) bool {
	return strings.ContainsAny(value, "<>/:：")
}
