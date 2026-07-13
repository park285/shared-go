package promptguard

import "regexp"

type rawRulepack struct {
	Version int       `yaml:"version"`
	Kind    string    `yaml:"kind"`
	Policy  rawPolicy `yaml:"policy"`
	Rules   []rawRule `yaml:"rules"`
}

type rawPolicy struct {
	ReviewThreshold    float64            `yaml:"review_threshold"`
	BlockThreshold     float64            `yaml:"block_threshold"`
	MinBlockFamilies   int                `yaml:"min_block_families"`
	SegmentMultipliers map[string]float64 `yaml:"segment_multipliers"`
	ViewMultipliers    map[string]float64 `yaml:"view_multipliers"`
}

type rawRule struct {
	ID             string   `yaml:"id"`
	Family         string   `yaml:"family"`
	Type           string   `yaml:"type"`
	Action         string   `yaml:"action"`
	View           string   `yaml:"view"`
	Segments       []string `yaml:"segments"`
	Pattern        string   `yaml:"pattern"`
	Phrases        []string `yaml:"phrases"`
	Weight         float64  `yaml:"weight"`
	MatchMode      string   `yaml:"match_mode"`
	MaxOccurrences int      `yaml:"max_occurrences"`
}

type compiledPolicy struct {
	ReviewThreshold    float64
	BlockThreshold     float64
	MinBlockFamilies   int
	SegmentMultipliers map[segmentKind]float64
	ViewMultipliers    map[string]float64
}

type compiledRule struct {
	ID                    string
	Family                string
	Type                  string
	Action                string
	View                  string
	Segments              map[segmentKind]struct{}
	Pattern               *regexp.Regexp
	Phrases               []string
	Weight                float64
	MatchMode             string
	MaxOccurrences        int
	RequiredLiteralGroups [][]string
}

type compiledPack struct {
	Version int
	Kind    string
	Policy  compiledPolicy
	Rules   []compiledRule
}

type compiledRulepackSet struct {
	Version int
	Policy  compiledPolicy
	Packs   []compiledPack
	Digest  string
}

type ruleSelectors struct {
	action   string
	view     string
	segments map[segmentKind]struct{}
}

const (
	hitActionScore       = "score"
	viewNorm             = "norm"
	viewRaw              = "raw"
	viewJoined           = "joined"
	viewAggregateNorm    = "aggregate_norm"
	viewAggregateJoined  = "aggregate_joined"
	ruleTypeRegex        = "regex"
	ruleTypePhrase       = "phrases"
	rulepackKindPolicy   = "policy"
	rulepackKindRules    = "rules"
	phraseMatchToken     = "token"
	phraseMatchSubstring = "substring"
)
