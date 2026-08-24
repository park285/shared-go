package outputguard

import "fmt"

// BoundGuard는 한 요청의 정규화된 protected text index를 소유한다.
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
		return nil, fmt.Errorf("protected index: %w", err)
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

	checkOutputSurfaces(text, g.index, &evaluation)

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
