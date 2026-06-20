package promptguard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	hitActionDampen = "dampen"
	hitActionBlock  = "block"
)

const ruleInputOversize = "input_oversize"

type Config struct {
	Enabled             bool
	Threshold           float64
	RulepacksDir        string
	RulepackFS          fs.FS
	RulepackRoot        string
	UseEmbeddedDefaults bool
	CacheMaxSize        int
	CacheTTL            time.Duration
	MaxInputBytes       int
	OnReview            func(Evaluation)
}

type Guard struct {
	cfg           Config
	logger        *slog.Logger
	packs         []compiledPack
	cache         *TTLCache[string, Evaluation]
	group         singleflight.Group
	onReview      func(Evaluation)
	maxInputBytes int
}

func NewGuard(cfg Config, logger *slog.Logger) (*Guard, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if !cfg.Enabled {
		return &Guard{cfg: cfg, logger: logger}, nil
	}

	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = time.Hour
	}

	if cfg.CacheMaxSize <= 0 {
		cfg.CacheMaxSize = 10000
	}

	if cfg.MaxInputBytes <= 0 {
		cfg.MaxInputBytes = 8 << 20
	}

	packs, err := loadConfiguredRulepacks(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("load rulepacks: %w", err)
	}

	return &Guard{
		cfg:           cfg,
		logger:        logger,
		packs:         packs,
		cache:         NewTTLCache[string, Evaluation](cfg.CacheMaxSize, cfg.CacheTTL),
		onReview:      cfg.OnReview,
		maxInputBytes: cfg.MaxInputBytes,
	}, nil
}

func loadConfiguredRulepacks(cfg Config, logger *slog.Logger) ([]compiledPack, error) {
	if cfg.RulepackFS != nil {
		root := strings.TrimSpace(cfg.RulepackRoot)
		if root == "" {
			root = "."
		}

		return loadRulepacksFS(cfg.RulepackFS, root, logger)
	}

	if strings.TrimSpace(cfg.RulepacksDir) != "" {
		return loadRulepacks(cfg.RulepacksDir, logger)
	}

	if cfg.UseEmbeddedDefaults {
		return loadRulepacksFS(defaultRulepackFS, defaultRulepacksRoot, logger)
	}

	return nil, fmt.Errorf("enabled guard requires RulepacksDir, RulepackFS, or UseEmbeddedDefaults")
}

func (g *Guard) Evaluate(input string) Evaluation {
	return g.evaluate(input, "")
}

// EnsureSafe는 입력이 악의적이면 에러를 반환한다.
func (g *Guard) EnsureSafe(input string) error {
	if blocked := g.blockedError(input, ""); blocked != nil {
		return blocked
	}

	return nil
}

// EnsureSafeFrom은 입력을 검사하고 리뷰 로깅용 source 태그를 부여한다.
func (g *Guard) EnsureSafeFrom(input, source string) error {
	if blocked := g.blockedError(input, source); blocked != nil {
		return blocked
	}

	return nil
}

func (g *Guard) blockedError(input, source string) *BlockedError {
	evaluation := g.evaluate(input, source)
	if !evaluation.Malicious() {
		return nil
	}

	rules := matchedRuleIDs(evaluation.Hits)
	if evaluation.OversizeBlocked {
		rules = append(rules, ruleInputOversize)
	}

	return &BlockedError{
		Score:     evaluation.Score,
		Threshold: evaluation.Threshold,
		Families:  distinctPositiveFamilies(evaluation.Hits),
		Rules:     rules,
		Source:    source,
	}
}

func (g *Guard) evaluate(input, source string) Evaluation {
	if g == nil || !g.cfg.Enabled {
		return Evaluation{Score: 0, Hits: nil, Threshold: math.Inf(1)}
	}

	if g.maxInputBytes > 0 && len(input) > g.maxInputBytes {
		return g.inputOversizeEvaluation(g.policy(), source, len(input))
	}

	key := cacheKey(input)
	if cached, ok := g.cache.Get(key); ok {
		cached.Source = source

		return cached
	}

	value, err, _ := g.group.Do(key, func() (any, error) {
		result := g.evaluateRaw(input)
		g.cache.Set(key, result)

		return result, nil
	})
	if err != nil {
		return g.fallbackEvaluation(g.policy(), source, err.Error())
	}

	if evaluation, ok := value.(Evaluation); ok {
		evaluation.Source = source
		g.logReviewEvaluation(&evaluation)

		return evaluation
	}

	return g.fallbackEvaluation(g.policy(), source, "evaluation type assertion failed")
}

func cacheKey(input string) string {
	sum := sha256.Sum256([]byte(input))

	return hex.EncodeToString(sum[:])
}

// fallbackEvaluation은 guard 평가가 실패했을 때의 conservative fallback이다.
// Review는 EnsureSafe/EnsureSafeFrom에서 차단되지 않으므로(Malicious는 Block만 true)
// 사용자를 hard-block하지 않으면서 운영이 인지할 수 있도록 Error 로깅 후 Review를 반환한다.
func (g *Guard) fallbackEvaluation(policy compiledPolicy, source, reason string) Evaluation {
	if g != nil && g.logger != nil {
		attrs := []any{slog.String("reason", reason)}

		if source != "" {
			attrs = append(attrs, slog.String("source", source))
		}

		g.logger.Error("guard_evaluation_fallback", attrs...)
	}

	return Evaluation{Decision: DecisionReview, Score: 0, Hits: nil, Threshold: policy.BlockThreshold, ReviewThreshold: policy.ReviewThreshold, Source: source}
}

func (g *Guard) inputOversizeEvaluation(policy compiledPolicy, source string, size int) Evaluation {
	if g != nil && g.logger != nil {
		attrs := []any{
			slog.String("reason", ruleInputOversize),
			slog.Int("size", size),
			slog.Int("max", g.maxInputBytes),
		}
		if source != "" {
			attrs = append(attrs, slog.String("source", source))
		}

		g.logger.Error("guard_input_oversize_blocked", attrs...)
	}

	return Evaluation{
		Decision:        DecisionBlock,
		Score:           policy.BlockThreshold,
		Hits:            nil,
		Threshold:       policy.BlockThreshold,
		ReviewThreshold: policy.ReviewThreshold,
		Source:          source,
		OversizeBlocked: true,
	}
}

func (g *Guard) policy() compiledPolicy {
	if len(g.packs) == 0 {
		return mergePolicies(nil, g.cfg.Threshold)
	}

	return mergePolicies(g.packs, g.cfg.Threshold)
}

func (g *Guard) threshold() float64 {
	return g.policy().BlockThreshold
}

func (g *Guard) evaluateRaw(input string) Evaluation {
	policy := g.policy()
	segments := splitTextSegments(input)

	segments = append(segments, decodedBase64Segments(input)...)

	return g.evaluateSegments(policy, segments)
}

func (g *Guard) evaluateSegments(policy compiledPolicy, segments []textSegment) Evaluation {
	state := evaluationState{hits: make([]Match, 0)}

	for _, pack := range g.packs {
		state.collectPackHits(pack, segments, policy)
	}

	families := distinctPositiveFamilies(state.hits)
	effectiveDampen := calculateEffectiveDampen(policy, state.positiveTotal, state.dampenTotal, len(families))
	score := math.Max(0, state.positiveTotal-effectiveDampen)
	decision := decideEvaluation(policy, score, len(families), state.hardPlainHit)

	return Evaluation{
		Decision:         decision,
		Score:            score,
		PositiveScore:    state.positiveTotal,
		DampenScore:      effectiveDampen,
		Hits:             state.hits,
		Threshold:        policy.BlockThreshold,
		ReviewThreshold:  policy.ReviewThreshold,
		DistinctFamilies: len(families),
	}
}

func (g *Guard) logReviewEvaluation(evaluation *Evaluation) {
	if g == nil || evaluation == nil || evaluation.Decision != DecisionReview {
		return
	}

	if g.logger != nil {
		attrs := []any{
			slog.Float64("score", evaluation.Score),
			slog.Float64("review_threshold", evaluation.ReviewThreshold),
			slog.Float64("block_threshold", evaluation.Threshold),
			slog.Int("distinct_families", evaluation.DistinctFamilies),
			slog.Any("families", distinctPositiveFamilies(evaluation.Hits)),
			slog.Any("rules", matchedRuleIDs(evaluation.Hits)),
		}
		if evaluation.Source != "" {
			attrs = append(attrs, slog.String("source", evaluation.Source))
		}

		g.logger.Warn("guard_review_detected", attrs...)
	}

	if g.onReview != nil {
		g.onReview(*evaluation)
	}
}

func matchedRuleIDs(hits []Match) []string {
	if len(hits) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(hits))

	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.Action == hitActionDampen || hit.ID == "" {
			continue
		}

		if _, ok := seen[hit.ID]; ok {
			continue
		}

		seen[hit.ID] = struct{}{}
		ids = append(ids, hit.ID)
	}

	return ids
}

type evaluationState struct {
	positiveTotal float64
	dampenTotal   float64
	hits          []Match
	hardPlainHit  bool
}

func (s *evaluationState) collectPackHits(pack compiledPack, segments []textSegment, policy compiledPolicy) {
	for ruleIndex := range pack.Rules {
		s.collectRuleHits(&pack.Rules[ruleIndex], segments, policy)
	}
}

func (s *evaluationState) collectRuleHits(rule *compiledRule, segments []textSegment, policy compiledPolicy) {
	for _, segment := range segments {
		s.applyHits(rule.matchSegment(segment, policy))
	}
}

func (s *evaluationState) applyHits(matched []Match) {
	for _, hit := range matched {
		s.hits = append(s.hits, hit)
		if hit.Action == hitActionDampen {
			s.dampenTotal += hit.Weight
			continue
		}

		s.positiveTotal += hit.Weight
		if hit.Action == hitActionBlock && hit.Segment == string(segmentPlain) {
			s.hardPlainHit = true
		}
	}
}

func calculateEffectiveDampen(policy compiledPolicy, positiveTotal, dampenTotal float64, familyCount int) float64 {
	limit := positiveTotal

	if familyCount >= policy.MinBlockFamilies {
		limit = positiveTotal * 0.5
	}

	return math.Min(dampenTotal, limit)
}

func decideEvaluation(policy compiledPolicy, score float64, familyCount int, hardPlainHit bool) Decision {
	switch {
	case hardPlainHit:
		return DecisionBlock
	case score >= policy.BlockThreshold && familyCount >= policy.MinBlockFamilies:
		return DecisionBlock
	case score >= policy.ReviewThreshold:
		return DecisionReview
	default:
		return DecisionAllow
	}
}
