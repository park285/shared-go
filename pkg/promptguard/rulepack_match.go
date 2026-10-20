package promptguard

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/internal/guardtext"
)

var allSegmentKinds = [...]segmentKind{segmentPlain, segmentQuote, segmentCode, segmentConfig}

func (r *compiledRule) appliesToSegment(kind segmentKind) bool {
	_, ok := r.Segments[kind]
	return ok
}

func (r *compiledRule) appliesToTextSegment(segment textSegment) bool {
	if !segment.Aggregate {
		return r.appliesToSegment(segment.Kind)
	}

	for _, kind := range allSegmentKinds {
		if segment.Kinds.contains(kind) && r.appliesToSegment(kind) {
			return true
		}
	}

	return false
}

// masksText는 예외 문맥이나 문장 경계를 가린 원문으로 적중을 다시 확인하는 규칙인지 나타낸다.
func (r *compiledRule) masksText() bool {
	return len(r.Exclude) > 0 || r.SentenceBreak > 0
}

func (r *compiledRule) matchSegment(segment textSegment, policy compiledPolicy, limit int) []Match {
	if !r.masksText() {
		return r.matchSegmentText(segment, policy, limit)
	}

	if segment.Aggregate {
		return r.matchAggregateExcluding(segment, policy, limit)
	}

	matches := r.matchSegmentText(segment, policy, limit)
	if len(matches) == 0 {
		return nil
	}

	var (
		views   Views
		changed bool
	)

	if segment.piece != nil {
		views, changed = segment.piece.maskedViews(r)
	} else {
		views, changed = r.maskViews(segment.Views)
	}

	if !changed {
		return matches
	}

	return r.matchSegmentText(textSegment{Kind: segment.Kind, Views: views}, policy, limit)
}

// maskViews는 가린 원문으로 세그먼트 view를 다시 만든다. 가린 범위가 없으면 원래 view를 돌려준다.
func (r *compiledRule) maskViews(views Views) (Views, bool) {
	masked, changed := r.maskExclusions(views.Raw)
	if !changed {
		return views, false
	}

	return normalizeViews(masked), true
}

// ruleExclusionMask는 글자·숫자·구분자·제어 문자가 아니어서 정규화와 결합 view에서 그대로 남는다.
const ruleExclusionMask = '\uE101'

// sentenceBoundaryPattern의 첫 묶음은 두 단어 이상인 문장 끝의 문장부호와 뒤따르는 공백·줄바꿈이다.
// 한 단어씩 끊은 우회(Ignore. Previous. Instructions.)와 줄 감싸기는 문장 경계로 보지 않는다.
var sentenceBoundaryPattern = regexp.MustCompile(`[^\s.!?。．！？]\s+[^\s.!?。．！？]+([.!?。．！？]\s+)`)

// maskExclusions는 예외 문맥(정규식에 묶음이 있으면 첫 묶음)이 차지한 원문 rune을 같은 수의 중립 문자로
// 가리고, SentenceBreak가 있으면 문장 경계를 그 수의 중립 문자로 바꾼다. 가린 rune은 이웃 단어를 잇지
// 않으므로 가린 근거만 사라지고, 문장을 건너는 일치는 경계만큼 간격을 더 써야 한다. 결합 세그먼트는
// 조각마다 따로 가리므로 조각 경계를 가로지른 예외 문맥은 쓰지 않는다.
func (r *compiledRule) maskExclusions(raw string) (string, bool) {
	type maskSpan struct{ start, end, runes int }

	var spans []maskSpan

	for index, exclusion := range r.Exclude {
		// 필수 리터럴이 없는 원문에서는 일치할 수 없으므로 정규식을 실행하지 않는다.
		if !containsAllLiteralGroupsASCIIFold(raw, r.ExcludeLiteralGroups[index]) {
			continue
		}

		if exclusion.NumSubexp() == 0 {
			for _, span := range exclusion.FindAllStringIndex(raw, -1) {
				spans = append(spans, maskSpan{start: span[0], end: span[1], runes: -1})
			}

			continue
		}

		// 묶음이 있으면 첫 묶음만 가린다. 나머지 일치는 예외 문맥이 성립하는 조건이어서, 부정한 동사만
		// 지우고 비밀값 명사는 같은 문장의 다른 요청 동사와 다시 짝지을 수 있게 남긴다.
		for _, span := range exclusion.FindAllStringSubmatchIndex(raw, -1) {
			if span[2] >= 0 {
				spans = append(spans, maskSpan{start: span[2], end: span[3], runes: -1})
			}
		}
	}

	if r.SentenceBreak > 0 {
		for _, span := range sentenceBoundaryPattern.FindAllStringSubmatchIndex(raw, -1) {
			spans = append(spans, maskSpan{start: span[2], end: span[3], runes: r.SentenceBreak})
		}
	}

	if len(spans) == 0 {
		return raw, false
	}

	slices.SortFunc(spans, func(left, right maskSpan) int { return left.start - right.start })

	var masked strings.Builder

	masked.Grow(len(raw))

	position := 0

	for _, span := range spans {
		if span.end <= position {
			continue
		}

		start := max(span.start, position)
		masked.WriteString(raw[position:start])

		runes := span.runes
		if runes < 0 {
			runes = utf8.RuneCountInString(raw[start:span.end])
		}

		for range runes {
			masked.WriteRune(ruleExclusionMask)
		}

		position = span.end
	}

	masked.WriteString(raw[position:])

	return masked.String(), true
}

// matchAggregateExcluding은 결합 세그먼트에서 마지막 조각 경계를 가로지르는 근거만 센다.
// 한쪽 조각 안의 근거는 조각별·이전 결합 평가가 온전한 문맥과 예외로 이미 판정했으므로,
// view 창이 예외 문맥을 잘라 낸 결합 세그먼트에서 다시 세지 않는다. 결합 view는 조각별 view를
// 이어 만들므로, 조각마다 예외 문맥과 문장 경계를 가린 view를 같은 방식으로 이어 근거가 남는지 본다.
func (r *compiledRule) matchAggregateExcluding(segment textSegment, policy compiledPolicy, limit int) []Match {
	matches := r.crossingMatches(segment, policy, limit)
	if len(matches) == 0 {
		return nil
	}

	right, rightChanged := segment.piece.maskedViews(r)
	left, leftChanged := r.maskedAggregateLeft(segment.rawChunks)

	if !leftChanged && !rightChanged {
		return matches
	}

	rebuilt := textSegment{Kind: segment.Kind, Kinds: segment.Kinds, Aggregate: true}
	leftRunes := utf8.RuneCountInString(left)

	if r.View == viewAggregateNorm {
		rebuilt.Views.Norm = left + guardBoundaryMarker + right.Norm
		rebuilt.rawChunks = []aggregateTailChunk{{NormRunes: leftRunes}}
	} else {
		rebuilt.Views.Joined = left + right.Joined
		rebuilt.rawChunks = []aggregateTailChunk{{JoinedRunes: leftRunes}}
	}

	if len(r.crossingMatches(rebuilt, policy, 1)) > 0 {
		return matches
	}

	return nil
}

// maskedAggregateLeft는 경계 앞 조각들의 가린 view를 결합 view와 같은 방식으로 잇는다. 교차 근거는
// 경계 앞 MaxMatchRunes 안에 있으므로 그 길이를 넘을 때까지 뒤쪽 조각만 잇는다.
func (r *compiledRule) maskedAggregateLeft(chunks []aggregateTailChunk) (string, bool) {
	separator := ""

	if r.View == viewAggregateNorm {
		separator = guardBoundaryMarker
	}

	var parts []string

	changed, runes := false, 0

	for index := len(chunks) - 1; index >= 0 && runes <= r.MaxMatchRunes; index-- {
		views, pieceChanged := chunks[index].piece.maskedViews(r)
		text := segmentView(textSegment{Views: views}, r.View)

		parts = append(parts, text)
		changed = changed || pieceChanged

		runes += utf8.RuneCountInString(text) + utf8.RuneCountInString(separator)
	}

	slices.Reverse(parts)

	return strings.Join(parts, separator), changed
}

// crossingMatches는 결합 view의 왼쪽 창과 오른쪽 조각에 걸친 근거를 limit개까지 찾는다.
// 교차 근거는 경계 앞뒤 MaxMatchRunes 안에 있으므로 그보다 한 rune 넓은 창만 검사한다. 잘라 낸
// 가장자리에서 앵커·단어 경계가 거짓으로 성립해도 그 일치는 길이 상한 때문에 경계를 넘지 못한다.
// 왼쪽 안의 근거와 겹친 교차 근거를 놓치지 않도록 왼쪽 근거의 다음 rune부터 다시 찾는다.
func (r *compiledRule) crossingMatches(segment textSegment, policy compiledPolicy, limit int) []Match {
	view, weight, ok := r.matchInput(segment, policy)
	if !ok {
		return nil
	}

	leftEnd, rightStart := segment.aggregateViewSplit(r.View, view)
	offset := byteOffsetBeforeRunes(view, leftEnd, r.MaxMatchRunes+1)
	text := view[offset : rightStart+byteOffsetAfterRunes(view[rightStart:], r.MaxMatchRunes+1)]

	leftEnd -= offset
	rightStart -= offset

	var matches []Match

	for from := 0; from < leftEnd && len(matches) < max(limit, 1); {
		location := r.Pattern.FindStringIndex(text[from:])
		if len(location) == 0 || from+location[0] >= leftEnd {
			break
		}

		start, end := from+location[0], from+location[1]
		if end > rightStart {
			matches = append(matches, newRuleMatch(r, segment, text[start:end], weight))
			from = end

			continue
		}

		_, size := utf8.DecodeRuneInString(text[start:])

		from = start + size
	}

	return matches
}

// aggregateViewSplit은 결합 view 문자열에서 왼쪽 창이 끝나는 위치와 오른쪽 조각이 시작하는 위치다.
// 두 위치 사이에는 norm view의 경계 표식이 있다.
func (segment textSegment) aggregateViewSplit(view, text string) (int, int) {
	runes := 0

	for _, chunk := range segment.rawChunks {
		switch view {
		case viewAggregateNorm:
			runes += chunk.NormRunes
		case viewAggregateJoined:
			runes += chunk.JoinedRunes
		default:
			runes += chunk.RawRunes
		}
	}

	leftEnd := byteOffsetAfterRunes(text, runes)

	if view == viewAggregateNorm {
		return leftEnd, min(len(text), leftEnd+len(guardBoundaryMarker))
	}

	return leftEnd, leftEnd
}

func byteOffsetBeforeRunes(text string, end, count int) int {
	for ; count > 0 && end > 0; count-- {
		_, size := utf8.DecodeLastRuneInString(text[:end])

		end -= size
	}

	return end
}

func byteOffsetAfterRunes(text string, count int) int {
	offset := 0

	for ; count > 0 && offset < len(text); count-- {
		_, size := utf8.DecodeRuneInString(text[offset:])

		offset += size
	}

	return offset
}

func (r *compiledRule) matchSegmentText(segment textSegment, policy compiledPolicy, limit int) []Match {
	text, weight, ok := r.matchInput(segment, policy)
	if !ok {
		return nil
	}

	switch r.Type {
	case ruleTypeRegex:
		return r.matchRegexSegment(segment, text, weight, limit)
	case ruleTypePhrase:
		return r.matchPhraseSegment(segment, text, weight, limit)
	default:
		return nil
	}
}

func (r *compiledRule) matchInput(segment textSegment, policy compiledPolicy) (string, float64, bool) {
	if !r.appliesToTextSegment(segment) {
		return "", 0, false
	}

	if segment.Aggregate && r.View != viewRaw && r.View != viewAggregateNorm && r.View != viewAggregateJoined {
		return "", 0, false
	}

	text := segmentView(segment, r.View)
	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}

	if !r.literalPrefilterPasses(segment, text) {
		return "", 0, false
	}

	weight := r.Weight * segmentWeightMultiplier(policy, segment) * policy.viewMultiplier(r.View)

	return text, weight, weight > 0
}

func (r *compiledRule) literalPrefilterPasses(segment textSegment, text string) bool {
	if r.View != viewRaw {
		slot := literalSlotNorm

		if r.View == viewJoined || r.View == viewAggregateJoined {
			slot = literalSlotJoined
		}

		if passed, ok := r.cachedLiteralsPass(segment, slot, text); ok {
			return passed
		}

		if len(r.RequiredLiteralBranches) > 0 {
			return containsAnyLiteralBranch(text, r.RequiredLiteralBranches)
		}

		return containsAllLiteralGroups(text, r.RequiredLiteralGroups)
	}

	slot := literalSlotNorm

	if segment.Aggregate {
		slot = literalSlotAggregateRawNorm
	}

	passed, ok := r.cachedLiteralsPass(segment, slot, segment.rawNormalizedView())
	if !ok {
		passed = containsAllLiteralGroups(segment.rawNormalizedView(), r.RequiredLiteralGroups)
	}

	if passed {
		return true
	}

	return len(r.RawCasePrefilter) > 0 &&
		containsAllLiteralGroups(segment.rawNormalizedView(), r.RawCaseStablePrefilter) &&
		containsASCIIUpper(text) &&
		containsAllLiteralGroupsASCIIFold(text, r.RawCasePrefilter)
}

func containsAnyLiteralBranch(text string, branches [][][]string) bool {
	for _, branch := range branches {
		if containsAllLiteralGroups(text, branch) {
			return true
		}
	}

	return false
}

func containsASCIIUpper(text string) bool {
	for index := range len(text) {
		if text[index] >= 'A' && text[index] <= 'Z' {
			return true
		}
	}

	return false
}

func containsAllLiteralGroupsASCIIFold(text string, groups [][]string) bool {
	for _, group := range groups {
		matched := false

		for _, literal := range group {
			if containsASCIIFold(text, literal) {
				matched = true
				break
			}
		}

		if !matched {
			return false
		}
	}

	return true
}

func containsASCIIFold(text, literal string) bool {
	if strings.Contains(text, literal) {
		return true
	}

	if literal == "" || len(literal) > len(text) {
		return literal == ""
	}

	for index := range len(literal) {
		if literal[index] >= 0x80 {
			return false
		}
	}

	for start := 0; start <= len(text)-len(literal); start++ {
		matched := true

		for offset := range len(literal) {
			if foldASCII(text[start+offset]) != foldASCII(literal[offset]) {
				matched = false
				break
			}
		}

		if matched {
			return true
		}
	}

	return false
}

func foldASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}

	return value
}

func (r *compiledRule) matchRegexSegment(segment textSegment, text string, weight float64, limit int) []Match {
	matches := r.matchRegexText(segment, text, weight, limit)
	if len(matches) > 0 || r.View != viewRaw {
		return matches
	}

	normalized := segment.normalizedRawFallback(text)
	if normalized == text {
		return nil
	}

	return r.matchRegexText(segment, normalized, weight, limit)
}

func (r *compiledRule) matchRegexText(segment textSegment, text string, weight float64, limit int) []Match {
	if limit <= 0 {
		limit = 1
	}

	if limit == 1 {
		location := r.Pattern.FindStringIndex(text)
		if location == nil {
			return nil
		}

		return []Match{newRuleMatch(r, segment, text[location[0]:location[1]], weight)}
	}

	spans := r.Pattern.FindAllString(text, limit)
	matches := make([]Match, 0, len(spans))

	for _, span := range spans {
		matches = append(matches, newRuleMatch(r, segment, span, weight))
	}

	return matches
}

func (r *compiledRule) matchPhraseSegment(segment textSegment, text string, weight float64, limit int) []Match {
	matches := r.matchPhraseText(segment, text, weight, limit)
	if len(matches) > 0 || r.View != viewRaw {
		return matches
	}

	normalized := segment.normalizedRawFallback(text)
	if normalized == text {
		return nil
	}

	return r.matchPhraseText(segment, normalized, weight, limit)
}

func (segment textSegment) rawNormalizedView() string {
	if segment.Aggregate {
		return segment.rawNorm
	}

	return segment.Views.Norm
}

// normalizedRawFallback은 raw 규칙이 원문 text에서 찾지 못했을 때 다시 볼 정규화 원문이다. 결합 세그먼트는
// 경계 창의 정규화 원문을 쓰고, 조각 세그먼트는 같은 원문을 규칙마다 다시 정규화하지 않도록 조각 캐시를 쓴다.
func (segment textSegment) normalizedRawFallback(text string) string {
	switch {
	case segment.Aggregate:
		return segment.rawNorm
	case segment.piece != nil && text == segment.piece.views.Raw:
		return segment.piece.normalizedRaw()
	default:
		return guardtext.NormalizeView(text)
	}
}

func (r *compiledRule) matchPhraseText(segment textSegment, text string, weight float64, limit int) []Match {
	if limit <= 0 {
		limit = 1
	}

	matches := make([]Match, 0, min(limit, len(r.Phrases)))
	for _, phrase := range r.Phrases {
		if !phraseMatches(text, phrase, r.MatchMode) {
			continue
		}

		matches = append(matches, newRuleMatch(r, segment, phrase, weight))
		if len(matches) >= limit {
			break
		}
	}

	return matches
}

func containsAnyLiteral(text string, literals []string) bool {
	for _, literal := range literals {
		if strings.Contains(text, literal) {
			return true
		}
	}

	return false
}

func containsAllLiteralGroups(text string, groups [][]string) bool {
	for _, group := range groups {
		if !containsAnyLiteral(text, group) {
			return false
		}
	}

	return true
}

func segmentView(segment textSegment, view string) string {
	switch view {
	case viewRaw:
		return segment.Views.Raw
	case viewJoined, viewAggregateJoined:
		return segment.Views.Joined
	default:
		return segment.Views.Norm
	}
}

func newRuleMatch(rule *compiledRule, segment textSegment, span string, weight float64) Match {
	return Match{
		ID:      rule.ID,
		Family:  rule.Family,
		Action:  rule.Action,
		View:    rule.View,
		Segment: string(segment.Kind),
		span:    trimSpan(span),
		Weight:  weight,
	}
}

func segmentWeightMultiplier(policy compiledPolicy, segment textSegment) float64 {
	if !segment.Aggregate || segment.Kinds == 0 {
		return policy.segmentMultiplier(segment.Kind)
	}

	multiplier := 0.0

	for _, kind := range allSegmentKinds {
		if segment.Kinds.contains(kind) {
			multiplier = max(multiplier, policy.segmentMultiplier(kind))
		}
	}

	return multiplier
}

func phraseMatches(text, phrase, mode string) bool {
	if mode != phraseMatchToken {
		return strings.Contains(text, phrase)
	}

	for start := 0; start <= len(text)-len(phrase); {
		index := strings.Index(text[start:], phrase)
		if index < 0 {
			return false
		}

		index += start

		beforeOK := index == 0 || !isTokenRuneBefore(text, index)
		after := index + len(phrase)
		afterOK := after == len(text) || !isTokenRuneAfter(text, after)

		if beforeOK && afterOK {
			return true
		}

		start = index + 1
	}

	return false
}

func isTokenRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])

	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isTokenRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])

	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func trimSpan(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 120 {
		return value
	}

	return value[:120]
}

func distinctPositiveFamilies(hits []Match) []string {
	set := make(map[string]struct{})

	for _, hit := range hits {
		if hit.Family == "" {
			continue
		}

		set[hit.Family] = struct{}{}
	}

	families := make([]string, 0, len(set))
	for family := range set {
		families = append(families, family)
	}

	slices.Sort(families)

	return families
}
