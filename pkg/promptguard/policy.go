package promptguard

func compilePolicy(raw *rawRulepack) compiledPolicy {
	if raw == nil {
		raw = &rawRulepack{}
	}

	blockThreshold := resolveBlockThreshold(raw)
	reviewThreshold := resolveReviewThreshold(raw, blockThreshold)
	minFamilies := resolveMinBlockFamilies(raw.Policy.MinBlockFamilies)
	segmentMultipliers := resolveSegmentMultipliers(raw.Policy.SegmentMultipliers)
	viewMultipliers := resolveViewMultipliers(raw.Policy.ViewMultipliers)

	return compiledPolicy{
		ReviewThreshold:    reviewThreshold,
		BlockThreshold:     blockThreshold,
		MinBlockFamilies:   minFamilies,
		SegmentMultipliers: segmentMultipliers,
		ViewMultipliers:    viewMultipliers,
	}
}

func resolveBlockThreshold(raw *rawRulepack) float64 {
	if raw.Policy.BlockThreshold > 0 {
		return raw.Policy.BlockThreshold
	}

	return 1.0
}

func resolveReviewThreshold(raw *rawRulepack, blockThreshold float64) float64 {
	if raw.Policy.ReviewThreshold > 0 {
		return raw.Policy.ReviewThreshold
	}

	if blockThreshold <= 0.55 {
		return blockThreshold * 0.75
	}

	return 0.55
}

func resolveMinBlockFamilies(minFamilies int) int {
	if minFamilies > 0 {
		return minFamilies
	}

	return 2
}

func resolveSegmentMultipliers(overrides map[string]float64) map[segmentKind]float64 {
	segmentMultipliers := map[segmentKind]float64{
		segmentPlain:  1.0,
		segmentQuote:  1.0,
		segmentCode:   1.0,
		segmentConfig: 1.0,
	}
	for key, value := range overrides {
		kind, ok := parseSegment(key)
		if ok && value > 0 {
			segmentMultipliers[kind] = value
		}
	}

	return segmentMultipliers
}

func resolveViewMultipliers(overrides map[string]float64) map[string]float64 {
	viewMultipliers := map[string]float64{
		viewRaw:             1.0,
		viewNorm:            1.0,
		viewJoined:          1.0,
		viewAggregateNorm:   1.0,
		viewAggregateJoined: 1.0,
	}

	for key, value := range overrides {
		view := normalizeView(key)
		if view != "" && value > 0 {
			viewMultipliers[view] = value
		}
	}

	return viewMultipliers
}

func (p compiledPolicy) segmentMultiplier(kind segmentKind) float64 {
	if value, ok := p.SegmentMultipliers[kind]; ok && value > 0 {
		return value
	}

	return 1.0
}

func (p compiledPolicy) viewMultiplier(view string) float64 {
	if value, ok := p.ViewMultipliers[view]; ok && value > 0 {
		return value
	}

	return 1.0
}
