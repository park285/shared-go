package promptguard

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	allowedPatternStyles = map[yaml.Style]struct{}{
		yaml.SingleQuotedStyle: {},
		yaml.LiteralStyle:      {},
	}
	genericBlockedPhrases = map[string]struct{}{
		"제한 없음":    {},
		"역할극":      {},
		"롤플레이":     {},
		"key":      {},
		"token":    {},
		"설정":       {},
		"메시지":      {},
		"roleplay": {},
	}
)

func lintRulepackNode(node *yaml.Node) error {
	if node == nil || len(node.Content) == 0 {
		return nil
	}

	document := node
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		document = node.Content[0]
	}

	if document.Kind != yaml.MappingNode {
		return errors.New("rulepack root must be a mapping")
	}

	rulesNode := findMappingValue(document, "rules")
	if rulesNode == nil || rulesNode.Kind != yaml.SequenceNode {
		return nil
	}

	for _, ruleNode := range rulesNode.Content {
		if err := lintRuleNode(ruleNode); err != nil {
			return fmt.Errorf("lint rule node: %w", err)
		}
	}

	return nil
}

func lintRuleNode(ruleNode *yaml.Node) error {
	if ruleNode == nil || ruleNode.Kind != yaml.MappingNode {
		return nil
	}

	if err := lintPatternNode(findMappingValue(ruleNode, "pattern")); err != nil {
		return fmt.Errorf("lint pattern: %w", err)
	}

	action := normalizeAction(scalarValue(findMappingValue(ruleNode, "action")))
	if action != hitActionBlock {
		return nil
	}

	if err := lintBlockedPhrases(findMappingValue(ruleNode, "phrases")); err != nil {
		return fmt.Errorf("lint blocked phrases: %w", err)
	}

	return nil
}

func lintPatternNode(patternNode *yaml.Node) error {
	if patternNode == nil {
		return nil
	}

	if _, ok := allowedPatternStyles[patternNode.Style]; !ok {
		return errors.New("pattern must use single-quoted scalar or literal block style")
	}

	if containsDoubleEscapedRegexClass(patternNode.Value) {
		return errors.New("pattern contains suspicious double-escaped regex classes; use single backslashes in YAML single-quoted scalars")
	}

	return nil
}

func containsDoubleEscapedRegexClass(pattern string) bool {
	return strings.Contains(pattern, `\\s`) ||
		strings.Contains(pattern, `\\w`) ||
		strings.Contains(pattern, `\\b`) ||
		strings.Contains(pattern, `\\d`) ||
		strings.Contains(pattern, `\\p`) ||
		strings.Contains(pattern, `\\P`)
}

func lintBlockedPhrases(phrasesNode *yaml.Node) error {
	if phrasesNode == nil || phrasesNode.Kind != yaml.SequenceNode {
		return nil
	}

	for _, phraseNode := range phrasesNode.Content {
		value := strings.ToLower(strings.TrimSpace(phraseNode.Value))
		if _, ok := genericBlockedPhrases[value]; ok {
			return fmt.Errorf("generic phrase %q cannot be used as a standalone block rule", phraseNode.Value)
		}
	}

	return nil
}

func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

func scalarValue(node *yaml.Node) string {
	if node == nil {
		return ""
	}

	return strings.TrimSpace(node.Value)
}
