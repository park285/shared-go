package promptguard

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
)

var errMissingRuleID = errors.New("invalid rule: missing id")

const maxRuleOccurrences = 64

// maxSentenceBreakRunes는 문장 경계마다 쓰는 중립 rune 수의 상한이다. 경계마다 원문이 그만큼 늘어나므로
// overlay 규칙이 검사 할당을 키우지 못하게 막는다.
const maxSentenceBreakRunes = 64

func compileRules(raw *rawRulepack) ([]compiledRule, error) {
	rules := make([]compiledRule, 0, len(raw.Rules))
	for i := range raw.Rules {
		compiled, err := compileRule(&raw.Rules[i])
		if err != nil {
			return nil, fmt.Errorf("compile rule %q: %w", raw.Rules[i].ID, err)
		}

		rules = append(rules, compiled)
	}

	return rules, nil
}

func compileRule(rule *rawRule) (compiledRule, error) {
	if strings.TrimSpace(rule.ID) == "" {
		return compiledRule{}, errMissingRuleID
	}

	selectors, err := compileRuleSelectors(rule)
	if err != nil {
		return compiledRule{}, fmt.Errorf("compile selectors: %w", err)
	}

	compiled := compiledRule{
		ID:             rule.ID,
		Family:         strings.TrimSpace(rule.Family),
		Type:           strings.ToLower(strings.TrimSpace(rule.Type)),
		Action:         selectors.action,
		View:           selectors.view,
		Segments:       selectors.segments,
		Weight:         rule.Weight,
		MatchMode:      strings.ToLower(strings.TrimSpace(rule.MatchMode)),
		MaxOccurrences: rule.MaxOccurrences,
		SentenceBreak:  rule.SentenceBreak,
	}
	if compiled.Family == "" {
		compiled.Family = compiled.ID
	}

	if compiled.Action == hitActionBlock && compiled.Weight == 0 {
		compiled.Weight = 1.0
	}

	if compiled.MaxOccurrences == 0 {
		compiled.MaxOccurrences = 1
	}

	if err = compileRuleMatcher(&compiled, rule); err != nil {
		return compiledRule{}, fmt.Errorf("compile matcher: %w", err)
	}

	if err = compileRuleMasking(&compiled, rule); err != nil {
		return compiledRule{}, fmt.Errorf("compile masking: %w", err)
	}

	if compiled.View == viewRaw && compiled.Type == ruleTypeRegex {
		compiled.RawCasePrefilter, compiled.RawCaseStablePrefilter = rawCasePrefilters(compiled.RequiredLiteralGroups)
	}

	compiled.RequiredLiteralGroups = normalizePrefilterLiteralGroups(compiled.RequiredLiteralGroups)
	compiled.AggregatePrefilter = bestRequiredLiteralGroup(compiled.RequiredLiteralGroups)

	if err = validateCompiledRule(&compiled); err != nil {
		return compiledRule{}, fmt.Errorf("validate rule: %w", err)
	}

	compiled.DecodePrograms, err = compileDecodePrograms(&compiled)
	if err != nil {
		return compiledRule{}, fmt.Errorf("compile decode matcher: %w", err)
	}

	return compiled, nil
}

func rawCasePrefilters(groups [][]string) ([][]string, [][]string) {
	var stable [][]string

	varies := false

	for _, group := range groups {
		groupVaries := false

		for _, literal := range group {
			lower := normalizeViews(literal).Norm
			upper := normalizeViews(strings.ToUpper(literal)).Norm

			if lower != upper {
				groupVaries = true
				varies = true

				break
			}
		}

		if !groupVaries {
			stable = append(stable, slices.Clone(group))
		}
	}

	if !varies {
		return nil, nil
	}

	return cloneLiteralGroups(groups), normalizePrefilterLiteralGroups(stable)
}

func cloneLiteralGroups(groups [][]string) [][]string {
	cloned := make([][]string, len(groups))
	for index, group := range groups {
		cloned[index] = slices.Clone(group)
	}

	return cloned
}

func normalizePrefilterLiteralGroups(groups [][]string) [][]string {
	normalized := make([][]string, 0, len(groups))
	for _, group := range groups {
		values := make([]string, 0, len(group))
		for _, literal := range group {
			value := normalizeViews(strings.ToLower(literal)).Norm
			if value != "" {
				values = append(values, value)
			}
		}

		if len(values) == 0 {
			continue
		}

		slices.Sort(values)

		normalized = append(normalized, slices.Compact(values))
	}

	return normalized
}

func compileRuleSelectors(rule *rawRule) (ruleSelectors, error) {
	action := normalizeAction(rule.Action)
	if strings.TrimSpace(rule.Action) != "" && action == "" {
		return ruleSelectors{}, fmt.Errorf("%s: unknown action %q", rule.ID, rule.Action)
	}

	view := normalizeView(rule.View)
	if strings.TrimSpace(rule.View) != "" && view == "" {
		return ruleSelectors{}, fmt.Errorf("%s: unknown view %q", rule.ID, rule.View)
	}

	if view == "" {
		view = viewNorm
	}

	segments, err := compileSegments(rule.Segments, action)
	if err != nil {
		return ruleSelectors{}, fmt.Errorf("%s: %w", rule.ID, err)
	}

	return ruleSelectors{
		action:   action,
		view:     view,
		segments: segments,
	}, nil
}

func compileRuleMatcher(compiled *compiledRule, rule *rawRule) error {
	switch compiled.Type {
	case ruleTypeRegex:
		if err := assignRegexMatcher(compiled, rule); err != nil {
			return fmt.Errorf("assign regex matcher: %w", err)
		}

		return nil
	case ruleTypePhrase:
		if err := assignPhraseMatcher(compiled, rule); err != nil {
			return fmt.Errorf("assign phrase matcher: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("unknown rule type %q", rule.Type)
	}
}

func assignRegexMatcher(compiled *compiledRule, rule *rawRule) error {
	if strings.TrimSpace(rule.Pattern) == "" {
		return fmt.Errorf("invalid regex rule %s", rule.ID)
	}

	pattern, err := regexp.Compile("(?i)" + rule.Pattern)
	if err != nil {
		return fmt.Errorf("compile regex %s: %w", rule.ID, err)
	}

	compiled.Pattern = pattern
	compiled.RequiredLiteralGroups = requiredRegexLiteralGroups(pattern)
	compiled.RequiredLiteralBranches = requiredRegexLiteralBranches(pattern)

	return nil
}

// compileRuleMasking은 예외 문맥과 문장 경계를 가릴 규칙의 예외 정규식과 최대 일치 길이를 준비한다.
func compileRuleMasking(compiled *compiledRule, rule *rawRule) error {
	exclusions, err := compileRuleExclusions(rule)
	if err != nil {
		return fmt.Errorf("compile exclusions: %w", err)
	}

	compiled.Exclude = exclusions
	for _, exclusion := range exclusions {
		compiled.ExcludeLiteralGroups = append(compiled.ExcludeLiteralGroups, requiredRegexLiteralGroups(exclusion))
	}

	if !compiled.masksText() || compiled.Pattern == nil {
		return nil
	}

	compiled.MaskKey = fmt.Sprintf("%d %q", compiled.SentenceBreak, rule.Exclude)

	parsed, err := syntax.Parse(compiled.Pattern.String(), syntax.Perl)
	if err != nil {
		return fmt.Errorf("parse pattern width: %w", err)
	}

	compiled.MaxMatchRunes = regexMaxRunes(parsed)

	return nil
}

// validateRuleMasking은 가림 규칙이 결합 평가에서 쓸 수 있는 형태인지 확인한다. 결합 평가는 경계 앞뒤
// 최대 일치 길이 안에서만 교차 근거를 찾으므로 길이 상한이 있는 정규식만 예외 문맥과 문장 경계를 쓴다.
func validateRuleMasking(compiled *compiledRule) error {
	if compiled.SentenceBreak < 0 || compiled.SentenceBreak > maxSentenceBreakRunes {
		return fmt.Errorf("%s: sentence_break must be between 0 and %d", compiled.ID, maxSentenceBreakRunes)
	}

	if compiled.masksText() && (compiled.Type != ruleTypeRegex || compiled.View == viewRaw || compiled.MaxMatchRunes < 0) {
		return fmt.Errorf("%s: exclude and sentence_break require a bounded regex rule on a normalized view", compiled.ID)
	}

	return nil
}

// compileRuleExclusions는 규칙 적중을 정상 문맥으로 돌려놓는 원문 정규식을 컴파일한다.
// 원문의 대소문자와 문장부호로 단어 내부·식별자를 구분하므로 (?i)를 붙이지 않는다.
func compileRuleExclusions(rule *rawRule) ([]*regexp.Regexp, error) {
	if len(rule.Exclude) == 0 {
		return nil, nil
	}

	exclusions := make([]*regexp.Regexp, 0, len(rule.Exclude))
	for _, value := range rule.Exclude {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("empty exclusion in rule %s", rule.ID)
		}

		exclusion, err := regexp.Compile(value)
		if err != nil {
			return nil, fmt.Errorf("compile exclusion %s: %w", rule.ID, err)
		}

		if exclusion.MatchString("") {
			return nil, fmt.Errorf("exclusion in rule %s matches empty text", rule.ID)
		}

		exclusions = append(exclusions, exclusion)
	}

	return exclusions, nil
}

// regexMaxRunes는 정규식이 일치할 수 있는 최대 rune 수다. 반복 상한이 없으면 -1이다.
func regexMaxRunes(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1
	case syntax.OpCapture, syntax.OpQuest:
		return regexMaxRunes(re.Sub[0])
	case syntax.OpStar, syntax.OpPlus:
		if regexMaxRunes(re.Sub[0]) == 0 {
			return 0
		}

		return -1
	case syntax.OpRepeat:
		sub := regexMaxRunes(re.Sub[0])
		if sub == 0 {
			return 0
		}

		if sub < 0 || re.Max < 0 {
			return -1
		}

		return sub * re.Max
	case syntax.OpNoMatch, syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return 0
	case syntax.OpConcat, syntax.OpAlternate:
		total := 0

		for _, sub := range re.Sub {
			value := regexMaxRunes(sub)
			if value < 0 {
				return -1
			}

			if re.Op == syntax.OpConcat {
				total += value
			} else {
				total = max(total, value)
			}
		}

		return total
	default:
		return -1
	}
}

func requiredRegexLiteralBranches(pattern *regexp.Regexp) [][][]string {
	parsed, err := syntax.Parse(pattern.String(), syntax.Perl)
	if err != nil {
		return nil
	}

	return requiredAlternativeBranches(parsed)
}

func requiredAlternativeBranches(expression *syntax.Regexp) [][][]string {
	if expression == nil {
		return nil
	}

	if expression.Op == syntax.OpCapture {
		if len(expression.Sub) == 1 {
			return requiredAlternativeBranches(expression.Sub[0])
		}
	}

	if expression.Op != syntax.OpAlternate {
		groups := normalizePrefilterLiteralGroups(requiredLiteralGroups(expression))
		if len(groups) == 0 {
			return nil
		}

		return [][][]string{groups}
	}

	branches := make([][][]string, 0, len(expression.Sub))
	for _, alternative := range expression.Sub {
		groups := normalizePrefilterLiteralGroups(requiredLiteralGroups(alternative))
		if len(groups) == 0 {
			return nil
		}

		branches = append(branches, groups)
	}

	return branches
}

func assignPhraseMatcher(compiled *compiledRule, rule *rawRule) error {
	phrases := make([]string, 0, len(rule.Phrases))
	for _, phrase := range rule.Phrases {
		value := strings.TrimSpace(phrase)
		if value == "" {
			continue
		}

		views := normalizeViews(value)

		switch compiled.View {
		case viewRaw:
			phrases = append(phrases, strings.ToLower(views.Raw))
		case viewJoined, viewAggregateJoined:
			phrases = append(phrases, views.Joined)
		default:
			phrases = append(phrases, views.Norm)
		}
	}

	if len(phrases) == 0 {
		return fmt.Errorf("invalid phrases rule %s", rule.ID)
	}

	compiled.Phrases = phrases
	compiled.RequiredLiteralGroups = [][]string{slices.Clone(phrases)}

	return nil
}

func requiredRegexLiteralGroups(pattern *regexp.Regexp) [][]string {
	parsed, err := syntax.Parse(pattern.String(), syntax.Perl)
	if err != nil {
		return nil
	}

	groups := requiredLiteralGroups(parsed)
	for i := range groups {
		for j := range groups[i] {
			groups[i][j] = strings.ToLower(groups[i][j])
		}

		slices.Sort(groups[i])

		groups[i] = slices.Compact(groups[i])
	}

	return groups
}

func requiredLiteralGroups(expression *syntax.Regexp) [][]string {
	if expression == nil {
		return nil
	}

	switch expression.Op {
	case syntax.OpLiteral:
		if literals := literalExpression(expression); len(literals) > 0 {
			return [][]string{literals}
		}
	case syntax.OpCapture:
		return requiredSingleSubexpressionGroups(expression)
	case syntax.OpConcat:
		return requiredConcatLiteralGroups(expression.Sub)
	case syntax.OpAlternate:
		return requiredAlternateLiteralGroups(expression.Sub)
	case syntax.OpPlus:
		return requiredSingleSubexpressionGroups(expression)
	case syntax.OpRepeat:
		if expression.Min > 0 && len(expression.Sub) == 1 {
			return requiredLiteralGroups(expression.Sub[0])
		}
	case syntax.OpNoMatch,
		syntax.OpEmptyMatch,
		syntax.OpCharClass,
		syntax.OpAnyCharNotNL,
		syntax.OpAnyChar,
		syntax.OpBeginLine,
		syntax.OpEndLine,
		syntax.OpBeginText,
		syntax.OpEndText,
		syntax.OpWordBoundary,
		syntax.OpNoWordBoundary,
		syntax.OpStar,
		syntax.OpQuest:
		return nil
	}

	return nil
}

func literalExpression(expression *syntax.Regexp) []string {
	if len(expression.Rune) == 0 {
		return nil
	}

	return []string{string(expression.Rune)}
}

func requiredSingleSubexpressionGroups(expression *syntax.Regexp) [][]string {
	if len(expression.Sub) != 1 {
		return nil
	}

	return requiredLiteralGroups(expression.Sub[0])
}

func requiredConcatLiteralGroups(expressions []*syntax.Regexp) [][]string {
	groups := make([][]string, 0, len(expressions))
	for _, expression := range expressions {
		groups = append(groups, requiredLiteralGroups(expression)...)
	}

	return groups
}

func requiredAlternateLiteralGroups(expressions []*syntax.Regexp) [][]string {
	var combined []string

	for _, expression := range expressions {
		candidate := bestRequiredLiteralGroup(requiredLiteralGroups(expression))
		if len(candidate) == 0 {
			return nil
		}

		combined = append(combined, candidate...)
	}

	return [][]string{combined}
}

func bestRequiredLiteralGroup(groups [][]string) []string {
	var best []string

	bestLength := 0

	for _, group := range groups {
		minimum := minimumLiteralRunes(group)
		if minimum > bestLength {
			best = group
			bestLength = minimum
		}
	}

	return best
}

func minimumLiteralRunes(values []string) int {
	minimum := 0

	for i, value := range values {
		length := len([]rune(value))
		if i == 0 || length < minimum {
			minimum = length
		}
	}

	return minimum
}

func validateCompiledRule(compiled *compiledRule) error {
	if !finiteFloat(compiled.Weight) {
		return fmt.Errorf("%s: weight must be finite", compiled.ID)
	}

	if compiled.Weight < 0 {
		return fmt.Errorf("%s: negative weight is unsupported", compiled.ID)
	}

	if compiled.Action != hitActionBlock && compiled.Weight <= 0 {
		return fmt.Errorf("%s: non-block rule requires positive weight", compiled.ID)
	}

	if compiled.MaxOccurrences <= 0 {
		return fmt.Errorf("%s: max_occurrences must be positive", compiled.ID)
	}

	if compiled.MaxOccurrences > maxRuleOccurrences {
		return fmt.Errorf("%s: max_occurrences exceeds %d", compiled.ID, maxRuleOccurrences)
	}

	if err := validateRuleMasking(compiled); err != nil {
		return err
	}

	if compiled.Type == ruleTypePhrase {
		if compiled.MatchMode != phraseMatchToken && compiled.MatchMode != phraseMatchSubstring {
			return fmt.Errorf("%s: phrases require match_mode token or substring", compiled.ID)
		}
	} else if compiled.MatchMode != "" {
		return fmt.Errorf("%s: match_mode is only valid for phrases", compiled.ID)
	}

	return nil
}

func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", hitActionScore:
		return hitActionScore
	case hitActionBlock:
		return hitActionBlock
	default:
		return ""
	}
}

func normalizeView(view string) string {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "", viewNorm:
		return viewNorm
	case viewRaw:
		return viewRaw
	case viewJoined:
		return viewJoined
	case viewAggregateNorm:
		return viewAggregateNorm
	case viewAggregateJoined:
		return viewAggregateJoined
	default:
		return ""
	}
}

func compileSegments(values []string, action string) (map[segmentKind]struct{}, error) {
	if len(values) == 0 {
		if action == hitActionBlock {
			return map[segmentKind]struct{}{segmentPlain: {}}, nil
		}

		return map[segmentKind]struct{}{
			segmentPlain:  {},
			segmentQuote:  {},
			segmentCode:   {},
			segmentConfig: {},
		}, nil
	}

	segments := make(map[segmentKind]struct{}, len(values))
	for _, value := range values {
		kind, ok := parseSegment(value)
		if !ok {
			return nil, fmt.Errorf("unknown segment %q", value)
		}

		segments[kind] = struct{}{}
	}

	return segments, nil
}

func parseSegment(value string) (segmentKind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(segmentPlain):
		return segmentPlain, true
	case string(segmentQuote):
		return segmentQuote, true
	case string(segmentCode):
		return segmentCode, true
	case string(segmentConfig):
		return segmentConfig, true
	default:
		return "", false
	}
}
