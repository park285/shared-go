package promptguard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"slices"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	hitActionBlock = "block"
)

const (
	ruleInputOversize         = "input_oversize"
	ruleEvaluationFallback    = "evaluation_fallback"
	ruleSegmentBudgetExceeded = "segment_budget_exceeded"
	ruleDecodeIncomplete      = "decode_incomplete"
	maxLoggedMatchValues      = 16
)

type Config struct {
	Enabled             bool
	RulepacksDir        string
	RulepackFS          fs.FS
	RulepackRoot        string
	UseEmbeddedDefaults bool
	CacheMaxSize        int
	CacheTTL            time.Duration
	MaxInputBytes       int
	OnEvaluation        func(EvaluationEvent)
}

type Guard struct {
	cfg                 Config
	logger              *slog.Logger
	packs               []compiledPack
	cache               *TTLCache[string, Evaluation]
	group               singleflight.Group
	onEvaluation        func(EvaluationEvent)
	maxInputBytes       int
	policyDigest        string
	effectivePolicy     compiledPolicy
	rulepackVersion     int
	aggregateFilter     aggregatePrefilterSet
	decodedContextRunes int
	evaluateInputFn     func(string) (Evaluation, error)
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

	set, err := loadConfiguredRulepacks(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("load rulepacks: %w", err)
	}

	return &Guard{
		cfg:                 cfg,
		logger:              logger,
		packs:               set.Packs,
		cache:               NewTTLCache[string, Evaluation](cfg.CacheMaxSize, cfg.CacheTTL),
		onEvaluation:        cfg.OnEvaluation,
		maxInputBytes:       cfg.MaxInputBytes,
		policyDigest:        set.Digest,
		effectivePolicy:     set.Policy,
		rulepackVersion:     set.Version,
		aggregateFilter:     compileAggregatePrefilters(set.Packs),
		decodedContextRunes: requiredLiteralContextRunes(set.Packs),
	}, nil
}

func (g *Guard) Check(req CheckRequest) (Evaluation, error) {
	if !validCheckRequest(req) {
		return Evaluation{}, ErrInvalidCheckRequest
	}
	if g == nil || !g.cfg.Enabled {
		return Evaluation{}, ErrGuardUnavailable
	}

	evaluation := g.evaluate(req.Text, req.Source)
	if !enforcementRejects(req.Enforcement, evaluation.Decision) {
		return evaluation, nil
	}

	return evaluation, blockedErrorFromEvaluation(evaluation)
}

func blockedErrorFromEvaluation(evaluation Evaluation) *BlockedError {
	rules := matchedRuleIDs(evaluation.Hits)
	if evaluation.OversizeBlocked {
		rules = append(rules, ruleInputOversize)
	}
	if evaluation.FallbackBlocked {
		rules = append(rules, ruleEvaluationFallback)
	}
	if evaluation.SegmentBudgetExceeded {
		rules = append(rules, ruleSegmentBudgetExceeded)
	}
	if evaluation.DecodeIncomplete {
		rules = append(rules, ruleDecodeIncomplete)
	}

	return &BlockedError{
		Score:     evaluation.Score,
		Threshold: evaluation.Threshold,
		Families:  distinctPositiveFamilies(evaluation.Hits),
		Rules:     rules,
		Source:    string(evaluation.Source),
		Decision:  evaluation.Decision,
	}
}

func (g *Guard) evaluate(input string, source Source) Evaluation {
	if g == nil || !g.cfg.Enabled {
		return Evaluation{Score: 0, Hits: nil, Threshold: math.Inf(1)}
	}

	if g.maxInputBytes > 0 && len(input) > g.maxInputBytes {
		evaluation := g.inputOversizeEvaluation(g.policy(), source, len(input))
		g.observeEvaluation(evaluation, false, len(input))

		return cloneEvaluation(evaluation)
	}

	key := cacheKey(input)
	if g.cache == nil {
		evaluation := g.fallbackEvaluation(g.policy(), source, ruleEvaluationFallback)
		g.observeEvaluation(evaluation, false, len(input))

		return cloneEvaluation(evaluation)
	}
	if cached, ok := g.cache.Get(key); ok {
		cached = cloneEvaluation(cached)
		cached.Source = source
		g.observeEvaluation(cached, true, len(input))

		return cloneEvaluation(cached)
	}

	value, err, _ := g.group.Do(key, func() (any, error) {
		if cached, ok := g.cache.Get(key); ok {
			return cloneEvaluation(cached), nil
		}

		result, detectErr := g.detectEvaluation(input)
		if detectErr != nil {
			return nil, detectErr
		}
		result.Source = ""
		g.cache.Set(key, cloneEvaluation(result))

		return cloneEvaluation(result), nil
	})
	if err != nil {
		evaluation := g.fallbackEvaluation(g.policy(), source, err.Error())
		g.observeEvaluation(evaluation, false, len(input))

		return cloneEvaluation(evaluation)
	}

	if evaluation, ok := value.(Evaluation); ok {
		evaluation = cloneEvaluation(evaluation)
		evaluation.Source = source
		g.observeEvaluation(evaluation, false, len(input))

		return cloneEvaluation(evaluation)
	}

	evaluation := g.fallbackEvaluation(g.policy(), source, "evaluation type assertion failed")
	g.observeEvaluation(evaluation, false, len(input))

	return cloneEvaluation(evaluation)
}

func cacheKey(input string) string {
	sum := sha256.Sum256([]byte(input))

	return hex.EncodeToString(sum[:])
}

// fallbackEvaluation은 guard 평가가 실패했을 때의 conservative fallback이다.
func (g *Guard) fallbackEvaluation(policy compiledPolicy, source Source, _ string) Evaluation {
	if g != nil && g.logger != nil {
		attrs := []any{slog.String("reason", ruleEvaluationFallback)}

		if source != "" {
			attrs = append(attrs, slog.String("source", string(source)))
		}

		g.logger.Error("guard_evaluation_fallback", attrs...)
	}

	return Evaluation{
		Decision:         DecisionBlock,
		Score:            policy.BlockThreshold,
		Hits:             nil,
		Threshold:        policy.BlockThreshold,
		ReviewThreshold:  policy.ReviewThreshold,
		Source:           source,
		FallbackBlocked:  true,
		OversizeBlocked:  false,
		DistinctFamilies: 0,
	}
}

func (g *Guard) inputOversizeEvaluation(policy compiledPolicy, source Source, size int) Evaluation {
	if g != nil && g.logger != nil {
		attrs := []any{
			slog.String("reason", ruleInputOversize),
			slog.Int("size", size),
			slog.Int("max", g.maxInputBytes),
		}
		if source != "" {
			attrs = append(attrs, slog.String("source", string(source)))
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
	if g.effectivePolicy.BlockThreshold > 0 {
		return g.effectivePolicy
	}
	if len(g.packs) > 0 {
		return g.packs[0].Policy
	}

	return compilePolicy(&rawRulepack{Version: 3})
}

func (g *Guard) PolicyDigest() string {
	if g == nil {
		return ""
	}

	return g.policyDigest
}

func (g *Guard) detectEvaluation(input string) (Evaluation, error) {
	var (
		evaluation Evaluation
		err        error
	)
	if g.evaluateInputFn != nil {
		evaluation, err = g.evaluateInputFn(input)
	} else {
		evaluation = g.evaluateRaw(input)
	}
	if err != nil {
		return Evaluation{}, err
	}
	if !validDecision(evaluation.Decision) {
		return Evaluation{}, fmt.Errorf("invalid detector decision")
	}

	return evaluation, nil
}

func (g *Guard) evaluateRaw(input string) Evaluation {
	policy := g.policy()
	segments, budgetExceeded := buildEvaluationSegmentsFiltered(input, g.aggregateMayMatch)
	if budgetExceeded {
		return Evaluation{
			Decision:              DecisionBlock,
			Score:                 policy.BlockThreshold,
			Threshold:             policy.BlockThreshold,
			ReviewThreshold:       policy.ReviewThreshold,
			SegmentBudgetExceeded: true,
		}
	}
	decoded, status := g.decodedTextSegments(input)
	segments = append(segments, decoded...)
	evaluation := g.evaluateSegments(policy, segments)
	if status != 0 {
		evaluation.Decision = DecisionBlock
		evaluation.DecodeIncomplete = true
	}
	return evaluation
}

func (g *Guard) aggregateMayMatch(tail *aggregateTail, right textSegment) bool {
	return g.aggregateFilter.mayMatch(tail, right)
}

func (g *Guard) evaluateSegments(policy compiledPolicy, segments []textSegment) Evaluation {
	state := evaluationState{hits: make([]Match, 0)}

	for _, pack := range g.packs {
		state.collectPackHits(pack, segments, policy)
	}

	families := distinctPositiveFamilies(state.hits)
	score := math.Max(0, state.positiveTotal)
	decision := decideEvaluation(policy, score, len(families), state.hardBlockHit)

	return Evaluation{
		Decision:         decision,
		Score:            score,
		Hits:             state.hits,
		Threshold:        policy.BlockThreshold,
		ReviewThreshold:  policy.ReviewThreshold,
		DistinctFamilies: len(families),
	}
}

func (g *Guard) observeEvaluation(evaluation Evaluation, cacheHit bool, inputBytes int) {
	if g == nil {
		return
	}

	if evaluation.Decision == DecisionReview && g.logger != nil {
		families, truncatedFamilies := boundedLogValues(distinctPositiveFamilies(evaluation.Hits))
		rules, truncatedRules := boundedLogValues(matchedRuleIDs(evaluation.Hits))
		attrs := []any{
			slog.Float64("score", evaluation.Score),
			slog.Float64("review_threshold", evaluation.ReviewThreshold),
			slog.Float64("block_threshold", evaluation.Threshold),
			slog.Int("distinct_families", evaluation.DistinctFamilies),
			slog.Any("families", families),
			slog.Any("rules", rules),
			slog.Bool("cache_hit", cacheHit),
		}
		if truncatedFamilies > 0 {
			attrs = append(attrs, slog.Int("families_truncated", truncatedFamilies))
		}
		if truncatedRules > 0 {
			attrs = append(attrs, slog.Int("rules_truncated", truncatedRules))
		}
		if evaluation.Source != "" {
			attrs = append(attrs, slog.String("source", string(evaluation.Source)))
		}

		g.logger.Warn("guard_review_detected", attrs...)
	}

	if g.onEvaluation != nil {
		families := distinctPositiveFamilies(evaluation.Hits)
		rules := matchedRuleIDs(evaluation.Hits)
		if evaluation.DecodeIncomplete {
			rules = append(rules, ruleDecodeIncomplete)
		}
		slices.Sort(rules)
		g.onEvaluation(EvaluationEvent{
			Source:           evaluation.Source,
			Decision:         evaluation.Decision,
			CacheHit:         cacheHit,
			PolicyDigest:     g.policyDigest,
			Score:            evaluation.Score,
			Families:         slices.Clone(families),
			RuleIDs:          slices.Clone(rules),
			InputBytes:       inputBytes,
			DecodeIncomplete: evaluation.DecodeIncomplete,
		})
	}
}

func validCheckRequest(req CheckRequest) bool {
	if !validSource(req.Source) {
		return false
	}

	switch req.Enforcement {
	case EnforcementObserve, EnforcementInteractive, EnforcementPersistent:
		return true
	default:
		return false
	}
}

func validSource(source Source) bool {
	switch source {
	case SourceUserPrompt,
		SourcePromptBundle,
		SourceRetrievedMemory,
		SourceMemoryCandidate,
		SourceSessionPatch,
		SourceSimulationState,
		SourceLawContext,
		SourceSessionContext,
		SourceChatLog,
		SourceWebSearchResult,
		SourceImagePrompt:
		return true
	default:
		return false
	}
}

func enforcementRejects(enforcement Enforcement, decision Decision) bool {
	switch enforcement {
	case EnforcementObserve:
		return false
	case EnforcementInteractive:
		return decision == DecisionBlock
	case EnforcementPersistent:
		return decision == DecisionReview || decision == DecisionBlock
	default:
		return true
	}
}

func validDecision(decision Decision) bool {
	switch decision {
	case DecisionAllow, DecisionReview, DecisionBlock:
		return true
	default:
		return false
	}
}

func cloneEvaluation(evaluation Evaluation) Evaluation {
	evaluation.Hits = slices.Clone(evaluation.Hits)

	return evaluation
}

func boundedLogValues(values []string) ([]string, int) {
	values = slices.Clone(values)
	slices.Sort(values)
	values = slices.Compact(values)
	if len(values) <= maxLoggedMatchValues {
		return values, 0
	}

	return slices.Clone(values[:maxLoggedMatchValues]), len(values) - maxLoggedMatchValues
}

func matchedRuleIDs(hits []Match) []string {
	if len(hits) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(hits))

	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.ID == "" {
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
	hits          []Match
	hardBlockHit  bool
}

func (s *evaluationState) collectPackHits(pack compiledPack, segments []textSegment, policy compiledPolicy) {
	for ruleIndex := range pack.Rules {
		s.collectRuleHits(&pack.Rules[ruleIndex], segments, policy)
	}
}

func (s *evaluationState) collectRuleHits(rule *compiledRule, segments []textSegment, policy compiledPolicy) {
	remaining := rule.MaxOccurrences
	for _, segment := range segments {
		segmentLimit := remaining
		if segmentLimit <= 0 {
			segmentLimit = 1
		}
		matched := rule.matchSegment(segment, policy, segmentLimit)
		s.applyHits(rule, matched)
		if remaining > 0 {
			remaining -= len(matched)
			if remaining == 0 {
				return
			}
		}
	}
}

func (s *evaluationState) applyHits(rule *compiledRule, matched []Match) {
	for _, hit := range matched {
		s.hits = append(s.hits, hit)
		s.positiveTotal += hit.Weight
		if hit.Action == hitActionBlock {
			s.hardBlockHit = true
		}
	}
}

func decideEvaluation(policy compiledPolicy, score float64, familyCount int, hardBlockHit bool) Decision {
	switch {
	case hardBlockHit:
		return DecisionBlock
	case score >= policy.BlockThreshold && familyCount >= policy.MinBlockFamilies:
		return DecisionBlock
	case score >= policy.ReviewThreshold:
		return DecisionReview
	default:
		return DecisionAllow
	}
}
