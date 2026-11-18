package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

type StagedReply struct {
	irisdurable.ReplyIdentity

	Outcome         irisdurable.ReplyStageOutcome
	ID              int64
	Status          irisdurable.ReplyStatus
	ClientRequestID string
	Payload         []byte
	PayloadHash     string
	Attempts        int
}

func (s *Store) Stage(ctx context.Context, record irisdurable.ReplyRecord) (irisdurable.ReplyStageOutcome, error) {
	staged, err := s.StageRow(ctx, record)

	return staged.Outcome, err
}

func (s *Store) StageRow(ctx context.Context, record irisdurable.ReplyRecord) (StagedReply, error) {
	if record.MessageID == "" || record.Phase == "" || record.RoomID == "" || record.ClientRequestID == "" {
		return StagedReply{}, errors.New("pgstore: reply record requires messageID, phase, roomID and clientRequestID")
	}

	if len(record.Payload) == 0 {
		return StagedReply{}, errors.New("pgstore: reply record requires a payload")
	}

	if s.opts.ReplyRetention <= 0 {
		return StagedReply{}, errors.New("pgstore: stage reply requires Options.ReplyRetention")
	}

	var staged StagedReply

	err := s.inTransaction(ctx, "stage reply", func(db Querier) error {
		var stageErr error

		staged, stageErr = s.stageRow(ctx, db, record)

		return stageErr
	})
	if err != nil {
		return StagedReply{}, err
	}

	return staged, nil
}

func (s *Store) stageRow(ctx context.Context, db Querier, record irisdurable.ReplyRecord) (StagedReply, error) {
	if _, err := db.Exec(ctx, lockReplySequenceSQL, s.opts.Scope, record.MessageID, record.Phase); err != nil {
		return StagedReply{}, fmt.Errorf("pgstore: lock reply sequence %s/%s: %w", record.MessageID, record.Phase, err)
	}

	hash := payloadHash(record.Payload)
	staged := StagedReply{ReplyIdentity: record.ReplyIdentity}

	var (
		status   string
		payload  *string
		inserted bool
	)

	err := db.QueryRow(ctx, stageReplySQL,
		s.opts.Scope, record.MessageID, record.Phase, record.Ordinal,
		record.RoomID, record.ClientRequestID, string(record.Payload), hash,
		s.opts.ReplyRetention.Seconds(),
	).Scan(&staged.ID, &status, &staged.ClientRequestID, &payload, &staged.PayloadHash, &staged.Attempts, &inserted)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		staged.Outcome = irisdurable.ReplyOrdinalSuperseded

		return staged, nil
	case err != nil:
		return StagedReply{}, fmt.Errorf("pgstore: stage reply %s/%s/%d: %w", record.MessageID, record.Phase, record.Ordinal, err)
	}

	staged.Status = irisdurable.ReplyStatus(status)

	if payload != nil {
		staged.Payload = []byte(*payload)
	}

	switch {
	case inserted:
		staged.Outcome = irisdurable.ReplyStaged
	case staged.PayloadHash == hash:
		staged.Outcome = irisdurable.ReplyAlreadyStaged
	default:
		if _, err := db.Exec(ctx, markReplyPayloadDivergenceSQL, s.opts.Scope, record.MessageID, record.Phase, record.Ordinal); err != nil {
			return StagedReply{}, fmt.Errorf("pgstore: mark reply divergence %s/%s/%d: %w", record.MessageID, record.Phase, record.Ordinal, err)
		}

		staged.Outcome = irisdurable.ReplyPayloadDiverged
	}

	return staged, nil
}

func (s *Store) BeginAttempt(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyAttempt, error) {
	if err := s.requirePolicy("begin reply attempt", true, true, true); err != nil {
		return irisdurable.ReplyAttempt{}, err
	}

	token, err := newClaimToken()
	if err != nil {
		return irisdurable.ReplyAttempt{}, err
	}

	attempt := irisdurable.ReplyAttempt{ReplyIdentity: identity, ClaimToken: token}

	err = s.db.QueryRow(ctx, beginReplyAttemptSQL,
		s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal, token, s.opts.Lease.Seconds(),
		s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(),
	).Scan(&attempt.Attempt, &attempt.ClientRequestID)

	switch {
	case err == nil:
		return attempt, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, err)
	}

	var exists int

	existsErr := s.db.QueryRow(ctx, replyExistsSQL, s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal).Scan(&exists)

	switch {
	case errors.Is(existsErr, pgx.ErrNoRows):
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, ErrNotFound)
	case existsErr != nil:
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: inspect reply claimability %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, existsErr)
	}

	return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, irisdurable.ErrReplyNotClaimable)
}

var settleableStatuses = map[irisdurable.ReplyStatus]bool{
	irisdurable.ReplyStatusAccepted:             true,
	irisdurable.ReplyStatusRetryablePreDispatch: true,
	irisdurable.ReplyStatusOutcomeUnknown:       true,
	irisdurable.ReplyStatusDead:                 true,
	irisdurable.ReplyStatusPermanentConflict:    true,
}

func (s *Store) Settle(ctx context.Context, attempt irisdurable.ReplyAttempt, outcome irisdurable.ReplyOutcome) error {
	if attempt.ClaimToken == "" {
		return errors.New("pgstore: settle requires a claim token")
	}

	if !settleableStatuses[outcome.Status] {
		return fmt.Errorf("pgstore: %q is not a settle outcome", outcome.Status)
	}

	retryAfter := outcome.RetryAfter.Seconds()
	if retryAfter < 0 {
		retryAfter = 0
	}

	return s.execFenced(ctx, "settle reply", settleReplySQL,
		s.opts.Scope, attempt.MessageID, attempt.Phase, attempt.Ordinal, attempt.ClaimToken,
		string(outcome.Status), outcome.ClientRequestID, outcome.IrisRequestID, retryAfter, outcome.Progressed,
	)
}

func (s *Store) Inspect(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyState, error) {
	var (
		state  irisdurable.ReplyState
		status string
	)

	err := s.db.QueryRow(ctx, inspectReplySQL, s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal).
		Scan(&status, &state.ClientRequestID, &state.Attempts, &state.PayloadPresent)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return irisdurable.ReplyState{}, fmt.Errorf("pgstore: inspect reply %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, ErrNotFound)
	case err != nil:
		return irisdurable.ReplyState{}, fmt.Errorf("pgstore: inspect reply %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, err)
	}

	state.Status = irisdurable.ReplyStatus(status)

	return state, nil
}

func (s *Store) BackoffFor(attempt int) (time.Duration, error) {
	if s.opts.BackoffBase <= 0 {
		return 0, errors.New("pgstore: backoff requires Options.BackoffBase and BackoffMax")
	}

	return s.backoff(attempt), nil
}

func payloadHash(payload []byte) string {
	sum := sha256.Sum256(payload)

	return hex.EncodeToString(sum[:])
}
