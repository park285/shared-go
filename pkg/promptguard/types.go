package promptguard

import (
	"errors"
	"fmt"
)

type Decision string

const (
	DecisionAllow  Decision = "allow"
	DecisionReview Decision = "review"
	DecisionBlock  Decision = "block"
)

type Source string

const (
	SourceUserPrompt      Source = "user_prompt"
	SourcePromptBundle    Source = "prompt_bundle"
	SourceRetrievedMemory Source = "retrieved_memory"
	SourceMemoryCandidate Source = "memory_candidate"
	SourceSessionPatch    Source = "session_patch"
	SourceSimulationState Source = "simulation_state"
	SourceLawContext      Source = "law_context"
	SourceSessionContext  Source = "session_context"
	SourceChatLog         Source = "chat_log"
	SourceWebSearchResult Source = "web_search_result"
	SourceImagePrompt     Source = "image_prompt"
)

type Enforcement uint8

const (
	EnforcementUnspecified Enforcement = iota
	EnforcementObserve
	EnforcementInteractive
	EnforcementPersistent
)

type CheckRequest struct {
	Text        string
	Source      Source
	Enforcement Enforcement
}

type EvaluationEvent struct {
	Source           Source
	Decision         Decision
	CacheHit         bool
	PolicyDigest     string
	Score            float64
	Families         []string
	RuleIDs          []string
	InputBytes       int
	DecodeIncomplete bool
}

var (
	ErrInvalidCheckRequest = errors.New("invalid prompt guard check request")
	ErrGuardUnavailable    = errors.New("prompt guard unavailable")
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
	Decision              Decision `json:"decision"`
	Score                 float64  `json:"score"`
	Hits                  []Match  `json:"hits"`
	Threshold             float64  `json:"threshold"`
	ReviewThreshold       float64  `json:"review_threshold"`
	DistinctFamilies      int      `json:"distinct_families"`
	Source                Source   `json:"source,omitempty"`
	OversizeBlocked       bool     `json:"oversize_blocked"`
	FallbackBlocked       bool     `json:"fallback_blocked"`
	SegmentBudgetExceeded bool     `json:"segment_budget_exceeded"`
	DecodeIncomplete      bool     `json:"decode_incomplete"`
	DecodeLimits          []string `json:"decode_limits,omitempty"`
}

type BlockedError struct {
	Score     float64
	Threshold float64
	Families  []string
	Rules     []string
	Source    string
	Decision  Decision
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("input blocked by injection guard (score=%.2f, threshold=%.2f)", e.Score, e.Threshold)
}
