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

	result := guardtext.DecodeCandidatesWithContextForRules(input, g.decodedCandidateMayContribute)
	return textSegmentsFromDecodeResult(result)
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
	views := normalizeViews(candidate)
	segment := textSegment{Kind: segmentPlain, Views: views}
	policy := g.policy()
	for i := range g.packs {
		for j := range g.packs[i].Rules {
			rule := &g.packs[i].Rules[j]
			if len(rule.matchSegment(segment, policy, 1)) > 0 {
				return true
			}
		}
	}

	if len(candidate) > maxDecodedRuleFragmentBytes {
		return false
	}
	if decodedCandidateHasBoundarySyntax(views.Raw) {
		return true
	}
	for i := range g.packs {
		for j := range g.packs[i].Rules {
			rule := &g.packs[i].Rules[j]
			if decodedTextOverlapsRequiredLiterals(decodedContributionView(rule, views), rule.RequiredLiteralGroups) {
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

	for _, run := range decodedLiteralRuns(text) {
		minimum := 3
		if containsNonASCII(run) {
			minimum = 2
		}
		if utf8.RuneCountInString(run) < minimum {
			continue
		}
		if strings.HasPrefix(literal, run) || strings.HasSuffix(literal, run) {
			return true
		}
	}

	return false
}

func decodedLiteralRuns(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
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
