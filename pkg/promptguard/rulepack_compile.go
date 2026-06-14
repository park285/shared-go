package promptguard

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var errMissingRuleID = errors.New("invalid rule: missing id")

func compileRulepack(raw *rawRulepack) (compiledPack, error) {
	if raw == nil {
		return compiledPack{}, errors.New("rulepack is nil")
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
		Policy: policy,
		Rules:  rules,
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
		ID:       rule.ID,
		Family:   strings.TrimSpace(rule.Family),
		Type:     strings.ToLower(strings.TrimSpace(rule.Type)),
		Action:   selectors.action,
		View:     selectors.view,
		Segments: selectors.segments,
		Weight:   rule.Weight,
	}
	if compiled.Family == "" {
		compiled.Family = compiled.ID
	}

	if compiled.Action == hitActionBlock && compiled.Weight <= 0 {
		compiled.Weight = 1.0
	}

	if err := compileRuleMatcher(&compiled, rule); err != nil {
		return compiledRule{}, fmt.Errorf("compile matcher: %w", err)
	}

	if err := validateCompiledRule(&compiled); err != nil {
		return compiledRule{}, fmt.Errorf("validate rule: %w", err)
	}

	return compiled, nil
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
	case "regex":
		if err := assignRegexMatcher(compiled, rule); err != nil {
			return fmt.Errorf("assign regex matcher: %w", err)
		}

		return nil
	case "phrases":
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

	return nil
}

func assignPhraseMatcher(compiled *compiledRule, rule *rawRule) error {
	phrases := make([]string, 0, len(rule.Phrases))
	for _, phrase := range rule.Phrases {
		value := strings.TrimSpace(phrase)
		if value == "" {
			continue
		}

		phrases = append(phrases, strings.ToLower(value))
	}

	if len(phrases) == 0 {
		return fmt.Errorf("invalid phrases rule %s", rule.ID)
	}

	compiled.Phrases = phrases

	return nil
}

func validateCompiledRule(compiled *compiledRule) error {
	if compiled.Action != hitActionBlock && compiled.Weight <= 0 {
		return fmt.Errorf("%s: non-block rule requires positive weight", compiled.ID)
	}

	return nil
}

func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", hitActionScore:
		return hitActionScore
	case hitActionBlock:
		return hitActionBlock
	case "dampen", "negative":
		return hitActionDampen
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
	default:
		return ""
	}
}

func compileSegments(values []string, action string) (map[segmentKind]struct{}, error) {
	if len(values) == 0 {
		if action == "block" {
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
	case "plain":
		return segmentPlain, true
	case "quote":
		return segmentQuote, true
	case "code":
		return segmentCode, true
	case "config":
		return segmentConfig, true
	default:
		return "", false
	}
}
