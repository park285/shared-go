package promptguard

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

const (
	maxBase64Candidates         = 8
	maxDecodedRuleFragmentBytes = 14
)

func decodedTextSegments(input string) ([]textSegment, guardtext.DecodeStatus) {
	return textSegmentsFromDecodeResult(guardtext.DecodeCandidatesWithContext(input))
}

func (g *Guard) decodedTextSegments(input string) ([]textSegment, guardtext.DecodeStatus) {
	return g.decodedTextSegmentsWithRaw(input, Evaluation{}, nil)
}

func (g *Guard) decodedTextSegmentsWithRaw(input string, raw Evaluation, budget *guardtext.RuleRegionDecoder) ([]textSegment, guardtext.DecodeStatus) {
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
		decodedInputCannotContributeForGuard,
		budget,
	)
	scored, overflow := partitionDecodedSegments(result)

	overflow = appendUniqueDecodedSegment(scored, overflow, blockingCandidate)

	witnesses := g.blockingOverflowSegments(raw, scored, overflow)

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
	raw := guard.evaluateTextSegments(original)
	policy := guard.policy()

	if guard.evaluateSegmentsWithBase(policy, raw, []textSegment{decodedCandidateSegment(decoded)}).Decision != DecisionBlock {
		return false
	}

	boundedSegments := make([]textSegment, 0, len(bounded))

	for _, candidate := range bounded {
		boundedSegments = append(boundedSegments, decodedCandidateSegment(candidate))
	}

	return guard.evaluateSegmentsWithBase(policy, raw, boundedSegments).Decision != DecisionBlock
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

func (g *Guard) blockingOverflowSegments(raw Evaluation, scored, overflow []textSegment) []textSegment {
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

	// 원문은 이미 규칙별 근거로 집계되어 있으므로 후보 최소화가 원문 크기만큼 재검사하지 않는다.
	witnesses := make([]textSegment, start)
	copy(witnesses, overflow[:start])

	for index := range slices.Backward(witnesses) {
		withoutCurrent := make([]textSegment, len(witnesses)-1)
		copy(withoutCurrent, witnesses[:index])
		copy(withoutCurrent[index:], witnesses[index+1:])

		if g.segmentsBlock(raw, scored, withoutCurrent) {
			witnesses = withoutCurrent
		}
	}

	return witnesses
}

func (g *Guard) segmentsBlock(raw Evaluation, scored, overflow []textSegment) bool {
	segments := make([]textSegment, 0, len(scored)+len(overflow))

	segments = append(segments, scored...)
	segments = append(segments, overflow...)

	return g.evaluateSegmentsWithBase(g.policy(), raw, segments).Decision == DecisionBlock
}

func (g *Guard) decodedCandidateMayContribute(candidate string) bool {
	views := normalizeViews(candidate)
	segment := textSegment{Kind: segmentPlain, Views: views}
	shortFragment := len(candidate) <= maxDecodedRuleFragmentBytes

	if shortFragment && decodedCandidateHasBoundarySyntax(views.Raw) {
		return true
	}

	for i := range g.packs {
		for j := range g.packs[i].Rules {
			rule := &g.packs[i].Rules[j]
			// 리터럴 없는 사용자 규칙은 이 사전 필터로 무해함을 증명할 수 없다.
			if len(rule.RequiredLiteralGroups) == 0 {
				return true
			}

			text := decodedContributionView(rule, views)

			if shortFragment && decodedTextOverlapsRequiredLiterals(text, rule.RequiredLiteralGroups) {
				return true
			}

			if shortFragment && rule.View == viewRaw && containsASCIIUpper(views.Raw) &&
				decodedTextOverlapsRequiredLiterals(strings.ToLower(views.Raw), rule.RawCasePrefilter) {
				return true
			}

			if shortFragment {
				continue
			}

			if rule.View == viewRaw {
				if rule.literalPrefilterPasses(segment, views.Raw) {
					return true
				}

				continue
			}

			if containsAllLiteralGroups(text, rule.RequiredLiteralGroups) {
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
					if literalNearDecoded(combined, literal, decodedStart, decodedEnd, strings.Contains) {
						return true
					}
				}
			}

			if len(rule.RawCasePrefilter) == 0 || !containsASCIIUpper(combinedViews.Raw) {
				continue
			}

			for _, group := range rule.RawCasePrefilter {
				for _, literal := range group {
					if literalNearDecoded(combinedViews.Raw, literal, len(leftViews.Raw), len(leftDecodedViews.Raw), containsASCIIFold) {
						return true
					}
				}
			}
		}
	}

	return false
}

func literalNearDecoded(text, literal string, decodedStart, decodedEnd int, match func(string, string) bool) bool {
	if literal == "" || decodedStart < 0 || decodedStart > decodedEnd || decodedStart > len(text) {
		return false
	}

	decodedEnd = min(len(text), decodedEnd)

	windowStart := max(0, decodedStart-len(literal))
	windowEnd := min(len(text), decodedEnd+len(literal))

	return match(text[windowStart:windowEnd], literal)
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
