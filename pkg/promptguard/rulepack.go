package promptguard

import "regexp"

type rawRulepack struct {
	Version   int       `yaml:"version"`
	Threshold float64   `yaml:"threshold"`
	Policy    rawPolicy `yaml:"policy"`
	Rules     []rawRule `yaml:"rules"`
}

type rawPolicy struct {
	ReviewThreshold    float64            `yaml:"review_threshold"`
	BlockThreshold     float64            `yaml:"block_threshold"`
	MinBlockFamilies   int                `yaml:"min_block_families"`
	SegmentMultipliers map[string]float64 `yaml:"segment_multipliers"`
	ViewMultipliers    map[string]float64 `yaml:"view_multipliers"`
}

type rawRule struct {
	ID       string   `yaml:"id"`
	Family   string   `yaml:"family"`
	Type     string   `yaml:"type"`
	Action   string   `yaml:"action"`
	View     string   `yaml:"view"`
	Segments []string `yaml:"segments"`
	Pattern  string   `yaml:"pattern"`
	Phrases  []string `yaml:"phrases"`
	Weight   float64  `yaml:"weight"`
}

type compiledPolicy struct {
	ReviewThreshold    float64
	BlockThreshold     float64
	MinBlockFamilies   int
	SegmentMultipliers map[segmentKind]float64
	ViewMultipliers    map[string]float64
}

type compiledRule struct {
	ID       string
	Family   string
	Type     string
	Action   string
	View     string
	Segments map[segmentKind]struct{}
	Pattern  *regexp.Regexp
	Phrases  []string
	Weight   float64
}

type compiledPack struct {
	Policy compiledPolicy
	Rules  []compiledRule
}

type ruleSelectors struct {
	action   string
	view     string
	segments map[segmentKind]struct{}
}

const (
	hitActionScore = "score"
	viewNorm       = "norm"
	viewRaw        = "raw"
	viewJoined     = "joined"
)
