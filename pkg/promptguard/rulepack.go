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
	Exclude        []string `yaml:"exclude"`
	SentenceBreak  int      `yaml:"sentence_break"`
}

type compiledPolicy struct {
	ReviewThreshold    float64
	BlockThreshold     float64
	MinBlockFamilies   int
	SegmentMultipliers map[segmentKind]float64
	ViewMultipliers    map[string]float64
}

type compiledRule struct {
	ID                      string
	Family                  string
	Type                    string
	Action                  string
	View                    string
	Segments                map[segmentKind]struct{}
	Pattern                 *regexp.Regexp
	DecodePrograms          []compiledDecodePattern
	Phrases                 []string
	Weight                  float64
	MatchMode               string
	MaxOccurrences          int
	RequiredLiteralGroups   [][]string
	RequiredLiteralBranches [][][]string
	RawCasePrefilter        [][]string
	RawCaseStablePrefilter  [][]string
	AggregatePrefilter      []string
	Exclude                 []*regexp.Regexp
	// ExcludeLiteralGroups는 Exclude와 같은 순서로 각 예외 정규식이 일치하려면 원문에 있어야 하는 소문자 리터럴 묶음이다.
	ExcludeLiteralGroups [][][]string
	// SentenceBreak는 일치가 문장 경계를 건널 때 간격으로 세는 rune 수다. 0이면 경계를 따로 세지 않는다.
	SentenceBreak int
	// MaxMatchRunes는 예외 문맥이나 문장 경계를 가리는 규칙의 정규식이 일치할 수 있는 최대 rune 수다.
	MaxMatchRunes int
	// MaskKey는 예외 정규식과 문장 경계 간격을 나타낸다. 가림 설정이 같은 규칙은 가린 view를 공유한다.
	MaskKey string
	// requiredLiterals는 Guard가 모든 규칙의 필수 리터럴로 만든 오토마톤이고, requiredLiteralMasks는
	// 이 규칙의 대안별 필수 리터럴 묶음을 그 오토마톤의 리터럴 번호 집합으로 바꾼 값이다.
	requiredLiterals     *requiredLiteralFilter
	requiredLiteralMasks [][]requiredLiteralSet
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
