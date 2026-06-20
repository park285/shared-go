package promptguard

import "fmt"

type Decision string

const (
	DecisionAllow  Decision = "allow"
	DecisionReview Decision = "review"
	DecisionBlock  Decision = "block"
)

type Match struct {
	ID      string `json:"id"`
	Family  string `json:"family,omitempty"`
	Action  string `json:"action,omitempty"`
	View    string `json:"view,omitempty"`
	Segment string `json:"segment,omitempty"`
	span    string
	Weight  float64 `json:"weight"`
}

type Evaluation struct {
	Decision         Decision `json:"decision"`
	Score            float64  `json:"score"`
	PositiveScore    float64  `json:"positive_score,omitempty"`
	DampenScore      float64  `json:"dampen_score,omitempty"`
	Hits             []Match  `json:"hits"`
	Threshold        float64  `json:"threshold"`
	ReviewThreshold  float64  `json:"review_threshold,omitempty"`
	DistinctFamilies int      `json:"distinct_families,omitempty"`
	Source           string   `json:"source,omitempty"`
	OversizeBlocked  bool     `json:"oversize_blocked,omitempty"`
}

func (e *Evaluation) Malicious() bool {
	if e == nil {
		return false
	}

	if e.Decision != "" {
		return e.Decision == DecisionBlock
	}

	return e.Score >= e.Threshold
}

type BlockedError struct {
	Score     float64
	Threshold float64
	Families  []string
	Rules     []string
	Source    string
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("input blocked by injection guard (score=%.2f, threshold=%.2f)", e.Score, e.Threshold)
}
