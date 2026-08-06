package promptguard

import (
	"slices"
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
	return g.decodedTextSegmentsWithRaw(input, nil)
}

func (g *Guard) decodedTextSegmentsWithRaw(input string, rawSegments []textSegment) ([]textSegment, guardtext.DecodeStatus) {
	if g == nil {
		return decodedTextSegments(input)
	}
	if !guardtext.HasPotentialRuleDecodeSurface(input) {
		return nil, 0
	}

	result, blockingCandidate := guardtext.DecodeCandidatesWithContextForRuleOwnerAndBlockWitness(
		input,
		g,
		decodedCandidateMayContributeForGuard,
		decodedContextMayContributeForGuard,
		oversizedDecodedWouldBlockForGuard,
		decodedCandidateWouldBlockForGuard,
	)
	scored, overflow := partitionDecodedSegments(result)
	overflow = appendUniqueDecodedSegment(scored, overflow, blockingCandidate)
	witnesses := g.blockingOverflowSegments(rawSegments, scored, overflow)

	return append(scored, witnesses...), result.Status
}

func decodedCandidateMayContributeForGuard(guard *Guard, candidate string) bool {
	return guard.decodedCandidateMayContribute(candidate)
}

func decodedContextMayContributeForGuard(guard *Guard, input string, start, end int, decoded string) bool {
	return guard.decodedContextMayContribute(input, start, end, decoded)
}

func decodedCandidateWouldBlockForGuard(guard *Guard, candidate string) bool {
	return guard.evaluateSegments(guard.policy(), []textSegment{decodedCandidateSegment(candidate)}).Decision == DecisionBlock
}

func oversizedDecodedWouldBlockForGuard(guard *Guard, original, decoded string, bounded []string) bool {
	rawSegments, exceeded := buildEvaluationSegmentsFiltered(original, guard.aggregateMayMatch)
	if exceeded {
		return false
	}
	policy := guard.policy()
	fullSegments := append(slices.Clone(rawSegments), decodedCandidateSegment(decoded))
	if guard.evaluateSegments(policy, fullSegments).Decision != DecisionBlock {
		return false
	}

	boundedSegments := slices.Clone(rawSegments)
	for _, candidate := range bounded {
		boundedSegments = append(boundedSegments, decodedCandidateSegment(candidate))
	}

	return guard.evaluateSegments(policy, boundedSegments).Decision != DecisionBlock
}

func decodedCandidateSegment(candidate string) textSegment {
	return textSegment{Kind: segmentPlain, Views: normalizeViews(candidate)}
}

func textSegmentsFromDecodeResult(result guardtext.DecodeResult) ([]textSegment, guardtext.DecodeStatus) {
	scored, _ := partitionDecodedSegments(result)

	return scored, result.Status
}

func partitionDecodedSegments(result guardtext.DecodeResult) ([]textSegment, []textSegment) {
	scored := make([]textSegment, 0, min(len(result.Candidates), maxBase64Candidates))
	var overflow []textSegment
	for _, candidate := range result.Candidates {
		if !guardtext.DecodedCandidateFitsBudget(candidate) {
			continue
		}
		segment := decodedCandidateSegment(candidate)
		if len(scored) < maxBase64Candidates {
			scored = append(scored, segment)
			continue
		}
		overflow = append(overflow, segment)
	}

	return scored, overflow
}

func appendUniqueDecodedSegment(scored, overflow []textSegment, candidate string) []textSegment {
	if candidate == "" || !guardtext.DecodedCandidateFitsBudget(candidate) {
		return overflow
	}
	for _, segment := range scored {
		if segment.Views.Raw == candidate {
			return overflow
		}
	}
	for _, segment := range overflow {
		if segment.Views.Raw == candidate {
			return overflow
		}
	}

	return append(overflow, decodedCandidateSegment(candidate))
}

func (g *Guard) blockingOverflowSegments(raw, scored, overflow []textSegment) []textSegment {
	if len(overflow) == 0 || g.segmentsBlock(raw, scored, nil) {
		return nil
	}
	if !g.segmentsBlock(raw, scored, overflow) {
		return nil
	}

	start, end := 1, len(overflow)
	for start < end {
		middle := start + (end-start)/2
		if g.segmentsBlock(raw, scored, overflow[:middle]) {
			end = middle
		} else {
			start = middle + 1
		}
	}

	witnesses := slices.Clone(overflow[:start])
	for index := range slices.Backward(witnesses) {
		withoutCurrent := slices.Clone(witnesses[:index])
		withoutCurrent = append(withoutCurrent, witnesses[index+1:]...)
		if g.segmentsBlock(raw, scored, withoutCurrent) {
			witnesses = withoutCurrent
		}
	}

	return witnesses
}

func (g *Guard) segmentsBlock(raw, scored, overflow []textSegment) bool {
	segments := make([]textSegment, 0, len(raw)+len(scored)+len(overflow))
	segments = append(segments, raw...)
	segments = append(segments, scored...)
	segments = append(segments, overflow...)

	return g.evaluateSegments(g.policy(), segments).Decision == DecisionBlock
}

func (g *Guard) decodedCandidateMayContribute(candidate string) bool {
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

func (g *Guard) decodedContextMayContribute(input string, start, end int, decoded string) bool {
	if g == nil || start < 0 || start > end || end > len(input) || g.decodedContextRunes <= 0 {
		return false
	}

	left := lastRunes(input[:start], g.decodedContextRunes)
	right := firstRunes(input[end:], g.decodedContextRunes)
	if !g.decodedBoundaryCompletesLiteral(left, decoded, right) {
		return false
	}

	context := input[:start] + decoded + input[end:]

	return g.decodedCandidateMayContribute(context)
}

func (g *Guard) decodedBoundaryCompletesLiteral(left, decoded, right string) bool {
	combinedViews := normalizeViews(left + decoded + right)
	leftViews := normalizeViews(left)
	leftDecodedViews := normalizeViews(left + decoded)

	for packIndex := range g.packs {
		for ruleIndex := range g.packs[packIndex].Rules {
			rule := &g.packs[packIndex].Rules[ruleIndex]
			combined := decodedContributionView(rule, combinedViews)
			decodedStart := len(decodedContributionView(rule, leftViews))
			decodedEnd := len(decodedContributionView(rule, leftDecodedViews))
			for _, group := range rule.RequiredLiteralGroups {
				for _, literal := range group {
					if literalNearDecoded(combined, literal, decodedStart, decodedEnd) {
						return true
					}
				}
			}
		}
	}

	return false
}

func literalNearDecoded(text, literal string, decodedStart, decodedEnd int) bool {
	if literal == "" || decodedStart < 0 || decodedStart > decodedEnd || decodedStart > len(text) {
		return false
	}
	decodedEnd = min(len(text), decodedEnd)

	windowStart := max(0, decodedStart-len(literal))
	windowEnd := min(len(text), decodedEnd+len(literal))
	for offset := windowStart; offset <= windowEnd; {
		index := strings.Index(text[offset:windowEnd], literal)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(literal)
		if end >= decodedStart && start <= decodedEnd {
			return true
		}
		offset = start + 1
	}

	return false
}

func requiredLiteralContextRunes(packs []compiledPack) int {
	maximum := 0
	for packIndex := range packs {
		for ruleIndex := range packs[packIndex].Rules {
			for _, group := range packs[packIndex].Rules[ruleIndex].RequiredLiteralGroups {
				for _, literal := range group {
					maximum = max(maximum, utf8.RuneCountInString(literal))
				}
			}
		}
	}

	return max(0, maximum-1)
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
