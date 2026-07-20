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

func compileRulepack(raw *rawRulepack) (compiledPack, error) {
	if raw == nil {
		return compiledPack{}, errors.New("rulepack is nil")
	}
	if raw.Version != 3 {
		return compiledPack{}, fmt.Errorf("rulepack version must be 3")
	}

	policy := compilePolicy(raw)

	rules := make([]compiledRule, 0, len(raw.Rules))
	for i := range raw.Rules {
		compiled, err := compileRule(&raw.Rules[i])
		if err != nil {
			return compiledPack{}, fmt.Errorf("compile rule %q: %w", raw.Rules[i].ID, err)
		}

		rules = append(rules, compiled)
	}

	return compiledPack{
		Version: raw.Version,
		Kind:    strings.ToLower(strings.TrimSpace(raw.Kind)),
		Policy:  policy,
		Rules:   rules,
	}, nil
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

	if err := compileRuleMatcher(&compiled, rule); err != nil {
		return compiledRule{}, fmt.Errorf("compile matcher: %w", err)
	}
	if compiled.View == viewRaw && compiled.Type == ruleTypeRegex {
		compiled.RawCasePrefilter, compiled.RawCaseStablePrefilter = rawCasePrefilters(compiled.RequiredLiteralGroups)
	}
	compiled.RequiredLiteralGroups = normalizePrefilterLiteralGroups(compiled.RequiredLiteralGroups)
	compiled.AggregatePrefilter = bestRequiredLiteralGroup(compiled.RequiredLiteralGroups)

	if err := validateCompiledRule(&compiled); err != nil {
		return compiledRule{}, fmt.Errorf("validate rule: %w", err)
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
