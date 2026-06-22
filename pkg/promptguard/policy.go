package promptguard

import "maps"

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

	if raw.Threshold > 0 {
		return raw.Threshold
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
		segmentQuote:  0.25,
		segmentCode:   0.25,
		segmentConfig: 0.35,
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
		"raw":      1.0,
		"norm":     1.0,
		viewJoined: 0.85,
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

func mergePolicies(packs []compiledPack, overrideBlockThreshold float64) compiledPolicy {
	if len(packs) == 0 {
		merged := compilePolicy(&rawRulepack{})

		if overrideBlockThreshold > 0 {
			merged.BlockThreshold = overrideBlockThreshold
		}

		if merged.ReviewThreshold > merged.BlockThreshold {
			merged.ReviewThreshold = merged.BlockThreshold
		}

		return merged
	}

	merged := compiledPolicy{
		ReviewThreshold:    packs[0].Policy.ReviewThreshold,
		BlockThreshold:     packs[0].Policy.BlockThreshold,
		MinBlockFamilies:   packs[0].Policy.MinBlockFamilies,
		SegmentMultipliers: make(map[segmentKind]float64, len(packs[0].Policy.SegmentMultipliers)),
		ViewMultipliers:    make(map[string]float64, len(packs[0].Policy.ViewMultipliers)),
	}
	maps.Copy(merged.SegmentMultipliers, packs[0].Policy.SegmentMultipliers)

	maps.Copy(merged.ViewMultipliers, packs[0].Policy.ViewMultipliers)

	for _, pack := range packs {
		if pack.Policy.ReviewThreshold > merged.ReviewThreshold {
			merged.ReviewThreshold = pack.Policy.ReviewThreshold
		}

		if pack.Policy.BlockThreshold > merged.BlockThreshold {
			merged.BlockThreshold = pack.Policy.BlockThreshold
		}

		if pack.Policy.MinBlockFamilies > merged.MinBlockFamilies {
			merged.MinBlockFamilies = pack.Policy.MinBlockFamilies
		}

		maps.Copy(merged.SegmentMultipliers, pack.Policy.SegmentMultipliers)

		maps.Copy(merged.ViewMultipliers, pack.Policy.ViewMultipliers)
	}

	if overrideBlockThreshold > 0 {
		merged.BlockThreshold = overrideBlockThreshold
	}

	if merged.ReviewThreshold > merged.BlockThreshold {
		merged.ReviewThreshold = merged.BlockThreshold
	}

	return merged
}
