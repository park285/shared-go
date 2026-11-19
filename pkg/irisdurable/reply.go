package irisdurable

import (
	"context"
	"errors"
	"time"
)

type ReplyStatus string

const (
	ReplyStatusPending              ReplyStatus = "pending"
	ReplyStatusSubmitting           ReplyStatus = "submitting"
	ReplyStatusAccepted             ReplyStatus = "accepted"
	ReplyStatusRetryablePreDispatch ReplyStatus = "retryable_pre_dispatch"
	ReplyStatusOutcomeUnknown       ReplyStatus = "outcome_unknown"
	ReplyStatusDead                 ReplyStatus = "dead"
	ReplyStatusPermanentConflict    ReplyStatus = "permanent_conflict"
)

func (s ReplyStatus) Terminal() bool {
	return s == ReplyStatusDead || s == ReplyStatusPermanentConflict
}

func (s ReplyStatus) Resendable() bool {
	return s == ReplyStatusPending || s == ReplyStatusRetryablePreDispatch || s == ReplyStatusOutcomeUnknown
}

type ReplyIdentity struct {
	MessageID string
	Phase     string
	Ordinal   int
}

type ReplyRecord struct {
	ReplyIdentity

	RoomID          string
	ClientRequestID string
	Payload         []byte
}

type ReplyStageOutcome string

const (
	ReplyStaged            ReplyStageOutcome = "staged"
	ReplyAlreadyStaged     ReplyStageOutcome = "already_staged"
	ReplyPayloadDiverged   ReplyStageOutcome = "payload_diverged"
	ReplyOrdinalSuperseded ReplyStageOutcome = "ordinal_superseded"
)

type ReplyAttempt struct {
	ReplyIdentity

	ClaimToken      string
	Attempt         int
	ClientRequestID string
}

type ReplyOutcome struct {
	Status          ReplyStatus
	ClientRequestID string
	IrisRequestID   string
	RetryAfter      time.Duration
	Progressed      bool
}

type ReplyState struct {
	Status          ReplyStatus
	ClientRequestID string
	Attempts        int
	PayloadPresent  bool
}

var ErrReplyNotClaimable = errors.New("irisdurable: reply is not claimable")

type ReplyOutbox interface {
	Stage(ctx context.Context, record ReplyRecord) (ReplyStageOutcome, error)
	BeginAttempt(ctx context.Context, identity ReplyIdentity) (ReplyAttempt, error)
	Settle(ctx context.Context, attempt ReplyAttempt, outcome ReplyOutcome) error
	Inspect(ctx context.Context, identity ReplyIdentity) (ReplyState, error)
}
