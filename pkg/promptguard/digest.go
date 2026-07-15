package promptguard

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"

	sharedjson "github.com/park285/shared-go/pkg/json"
)

const EngineVersion = "promptguard-engine-v3.2.2"

type digestEntry[T any] struct {
	Name  string `json:"name"`
	Value T      `json:"value"`
}

type digestPolicy struct {
	ReviewThreshold    float64                `json:"review_threshold"`
	BlockThreshold     float64                `json:"block_threshold"`
	MinBlockFamilies   int                    `json:"min_block_families"`
	SegmentMultipliers []digestEntry[float64] `json:"segment_multipliers"`
	ViewMultipliers    []digestEntry[float64] `json:"view_multipliers"`
}

type digestRule struct {
	ID             string   `json:"id"`
	Family         string   `json:"family"`
	Type           string   `json:"type"`
	Action         string   `json:"action"`
	View           string   `json:"view"`
	Segments       []string `json:"segments"`
	Pattern        string   `json:"pattern,omitempty"`
	Phrases        []string `json:"phrases,omitempty"`
	Weight         float64  `json:"weight"`
	MatchMode      string   `json:"match_mode,omitempty"`
	MaxOccurrences int      `json:"max_occurrences,omitempty"`
}

type digestDocument struct {
	EngineVersion string       `json:"engine_version"`
	Rulepack      int          `json:"rulepack_version"`
	Policy        digestPolicy `json:"policy"`
	Rules         []digestRule `json:"rules"`
}

func computePolicyDigest(set compiledRulepackSet) (string, error) {
	document := digestDocument{
		EngineVersion: EngineVersion,
		Rulepack:      set.Version,
		Policy:        policyForDigest(set.Policy),
		Rules:         rulesForDigest(allRules(set.Packs)),
	}
	encoded, err := sharedjson.Marshal(document)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:]), nil
}

func policyForDigest(policy compiledPolicy) digestPolicy {
	return digestPolicy{
		ReviewThreshold:    policy.ReviewThreshold,
		BlockThreshold:     policy.BlockThreshold,
		MinBlockFamilies:   policy.MinBlockFamilies,
		SegmentMultipliers: floatMapForDigest(policy.SegmentMultipliers),
		ViewMultipliers:    floatMapForDigest(policy.ViewMultipliers),
	}
}

func floatMapForDigest[K ~string](values map[K]float64) []digestEntry[float64] {
	keys := make([]string, 0, len(values))
	byKey := make(map[string]float64, len(values))
	for key, value := range values {
		name := string(key)
		keys = append(keys, name)
		byKey[name] = value
	}
	sort.Strings(keys)

	entries := make([]digestEntry[float64], 0, len(keys))
	for _, key := range keys {
		entries = append(entries, digestEntry[float64]{Name: key, Value: byKey[key]})
	}

	return entries
}

func rulesForDigest(rules []compiledRule) []digestRule {
	result := make([]digestRule, 0, len(rules))
	for i := range rules {
		rule := &rules[i]
		segments := make([]string, 0, len(rule.Segments))
		for segment := range rule.Segments {
			segments = append(segments, string(segment))
		}
		sort.Strings(segments)
		phrases := slices.Clone(rule.Phrases)
		sort.Strings(phrases)
		pattern := ""
		if rule.Pattern != nil {
			pattern = rule.Pattern.String()
		}
		result = append(result, digestRule{
			ID:             rule.ID,
			Family:         rule.Family,
			Type:           rule.Type,
			Action:         rule.Action,
			View:           rule.View,
			Segments:       segments,
			Pattern:        pattern,
			Phrases:        phrases,
			Weight:         rule.Weight,
			MatchMode:      rule.MatchMode,
			MaxOccurrences: rule.MaxOccurrences,
		})
	}
	slices.SortFunc(result, func(left, right digestRule) int {
		if left.ID < right.ID {
			return -1
		}
		if left.ID > right.ID {
			return 1
		}

		return 0
	})

	return result
}
