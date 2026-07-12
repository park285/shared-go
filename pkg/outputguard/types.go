package outputguard

import "errors"

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionBlock Decision = "block"
)

type ReasonCode string

const (
	ReasonRoleBlock              ReasonCode = "role_block"
	ReasonSecretPattern          ReasonCode = "secret_pattern"
	ReasonProtectedTextOverlap   ReasonCode = "protected_text_overlap"
	ReasonProtectedInputOversize ReasonCode = "protected_input_oversize"
	ReasonOutputOversize         ReasonCode = "output_oversize"
)

type CheckRequest struct {
	Text           string   `json:"text"`
	ProtectedTexts []string `json:"protected_texts,omitempty"`
}

type Evaluation struct {
	Decision    Decision     `json:"decision"`
	ReasonCodes []ReasonCode `json:"reason_codes,omitempty"`
	RuleIDs     []string     `json:"rule_ids,omitempty"`
	OutputBytes int          `json:"output_bytes"`
}

var ErrRestrictedGeneratedText = errors.New("generated answer contains restricted output")
