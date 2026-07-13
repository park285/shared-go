package outputguard

// BoundGuard owns one request's protected text index. It is deliberately not
// reusable across requests because the index retains normalized protected text.
type BoundGuard struct{ index *protectedIndex }

func (g *Guard) Bind(protectedTexts []string) (*BoundGuard, error) {
	if g == nil {
		return nil, ErrInvalidProtectedTexts
	}
	protected, ok, oversize := validateProtectedTexts(protectedTexts)
	if !ok || oversize {
		return nil, ErrInvalidProtectedTexts
	}
	index, err := newProtectedIndex(protected)
	if err != nil {
		return nil, err
	}
	return &BoundGuard{index: index}, nil
}

func (g *BoundGuard) Check(text string) Evaluation {
	evaluation := Evaluation{Decision: DecisionAllow, OutputBytes: len(text)}
	if g == nil || g.index == nil {
		evaluation.Decision = DecisionBlock
		evaluation.ReasonCodes = []ReasonCode{ReasonProtectedInputInvalid}
		return evaluation
	}
	if len(text) > maxOutputBytes {
		evaluation.Decision = DecisionBlock
		evaluation.ReasonCodes = []ReasonCode{ReasonOutputOversize}
		return evaluation
	}
	surfaces, incomplete := outputSurfaces(text)
	collectRestrictedMatches(surfaces, &evaluation)
	if incomplete {
		appendReason(&evaluation, ReasonDecodeIncomplete)
	}
	if protectedOverlap(surfaces, g.index) {
		appendReason(&evaluation, ReasonProtectedTextOverlap)
	}
	if len(evaluation.ReasonCodes) > 0 {
		evaluation.Decision = DecisionBlock
	}
	return evaluation
}

func (g *BoundGuard) Validate(text string) error {
	if g.Check(text).Decision == DecisionBlock {
		return ErrRestrictedGeneratedText
	}
	return nil
}
