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

// StagedReply는 StageRow가 돌려주는, stage 직후의 저장된 행이다. Outcome이
// ReplyOrdinalSuperseded면 행이 없으므로 나머지 항목은 0값이다.
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

// Stage는 응답을 outbox에 멱등하게 기록한다. 같은 식별자에 다른 payload가 오면 저장본을 유지하고
// ReplyPayloadDiverged를 반환한다.
func (s *Store) Stage(ctx context.Context, record irisdurable.ReplyRecord) (irisdurable.ReplyStageOutcome, error) {
	staged, err := s.StageRow(ctx, record)

	return staged.Outcome, err
}

// StageRow는 Stage와 같은 일을 하고 저장된 행을 함께 돌려준다. 발송 여부를 저장본의 상태와
// 시도 횟수로 판단하는 호출자가 Stage 뒤에 Inspect를 한 번 더 하지 않게 한다.
func (s *Store) StageRow(ctx context.Context, record irisdurable.ReplyRecord) (StagedReply, error) {
	if record.MessageID == "" || record.Phase == "" || record.RoomID == "" || record.ClientRequestID == "" {
		return StagedReply{}, errors.New("pgstore: reply record requires messageID, phase, roomID and clientRequestID")
	}

	if len(record.Payload) == 0 {
		return StagedReply{}, errors.New("pgstore: reply record requires a payload")
	}

	hash := payloadHash(record.Payload)
	staged := StagedReply{ReplyIdentity: record.ReplyIdentity}

	var (
		status   string
		payload  *string
		inserted bool
	)

	err := s.db.QueryRow(ctx, queryStageReply,
		s.opts.Scope, record.MessageID, record.Phase, record.Ordinal,
		record.RoomID, record.ClientRequestID, string(record.Payload), hash,
		s.opts.ReplyRetention.Seconds(),
	).Scan(&staged.ID, &status, &staged.ClientRequestID, &payload, &staged.PayloadHash, &staged.Attempts, &inserted)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// 삽입도 조회도 행을 내지 않는 유일한 경우는 후속 ordinal 가드가 새 행을 막은 것이다.
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
		if _, err := s.db.Exec(ctx, queryMarkReplyPayloadDivergence, s.opts.Scope, record.MessageID, record.Phase, record.Ordinal); err != nil {
			return StagedReply{}, fmt.Errorf("pgstore: mark reply divergence %s/%s/%d: %w", record.MessageID, record.Phase, record.Ordinal, err)
		}

		staged.Outcome = irisdurable.ReplyPayloadDiverged
	}

	return staged, nil
}

// BeginAttempt는 재발송 가능한 행의 소유권을 잡는다. 종단이거나 lease가 살아 있으면
// irisdurable.ErrReplyNotClaimable을, 행이 없으면 ErrNotFound를 반환한다.
func (s *Store) BeginAttempt(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyAttempt, error) {
	token, err := newClaimToken()
	if err != nil {
		return irisdurable.ReplyAttempt{}, err
	}

	attempt := irisdurable.ReplyAttempt{ReplyIdentity: identity, ClaimToken: token}

	err = s.db.QueryRow(ctx, queryBeginReplyAttempt,
		s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal, token, s.opts.Lease.Seconds(),
	).Scan(&attempt.Attempt, &attempt.ClientRequestID)

	switch {
	case err == nil:
		return attempt, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, err)
	}

	var exists int

	existsErr := s.db.QueryRow(ctx, queryReplyExists, s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal).Scan(&exists)

	switch {
	case errors.Is(existsErr, pgx.ErrNoRows):
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, ErrNotFound)
	case existsErr != nil:
		return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: inspect reply claimability %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, existsErr)
	}

	return irisdurable.ReplyAttempt{}, fmt.Errorf("pgstore: begin reply attempt %s/%s/%d: %w", identity.MessageID, identity.Phase, identity.Ordinal, irisdurable.ErrReplyNotClaimable)
}

// RenewReply는 시도 중인 행의 lease를 Options.Lease만큼 연장한다. Iris 호출이 lease보다 오래
// 걸릴 수 있는 호출자가 heartbeat로 소유권을 유지하는 경로이고, RenewInbox와 대칭이다. 이미
// lease를 잃었거나 다른 시도가 행을 가져갔으면 ErrClaimLost를 반환한다.
func (s *Store) RenewReply(ctx context.Context, attempt irisdurable.ReplyAttempt) error {
	if attempt.ClaimToken == "" {
		return errors.New("pgstore: renew requires a claim token")
	}

	return s.execFenced(ctx, "renew reply lease", queryRenewReplyLease,
		s.opts.Scope, attempt.MessageID, attempt.Phase, attempt.Ordinal, attempt.ClaimToken, s.opts.Lease.Seconds(),
	)
}

// Settle이 받아들이는 결과 상태다. 시도 결과가 아닌 pending·submitting과, 이유가 필요해
// ManualReviewReply가 따로 기록하는 manual_review는 제외한다.
var settleableStatuses = map[irisdurable.ReplyStatus]bool{
	irisdurable.ReplyStatusAccepted:             true,
	irisdurable.ReplyStatusRetryablePreDispatch: true,
	irisdurable.ReplyStatusOutcomeUnknown:       true,
	irisdurable.ReplyStatusDead:                 true,
	irisdurable.ReplyStatusPermanentConflict:    true,
}

// Settle은 같은 ClaimToken을 쥔 시도만 상태를 바꾼다.
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

	return s.execFenced(ctx, "settle reply", querySettleReply,
		s.opts.Scope, attempt.MessageID, attempt.Phase, attempt.Ordinal, attempt.ClaimToken,
		string(outcome.Status), outcome.ClientRequestID, outcome.IrisRequestID, retryAfter, outcome.Progressed,
	)
}

// ManualReviewReply는 자동으로 판정할 수 없는 응답을 사람이 볼 안전 경계 상태로 보낸다.
// 운영자가 replay 또는 discard를 고를 수 있도록 payload는 남긴다.
func (s *Store) ManualReviewReply(ctx context.Context, attempt irisdurable.ReplyAttempt, reason string) error {
	if attempt.ClaimToken == "" {
		return errors.New("pgstore: manual review requires a claim token")
	}

	if reason == "" {
		return errors.New("pgstore: manual review requires a reason")
	}

	return s.execFenced(ctx, "manual review reply", queryManualReviewReply,
		s.opts.Scope, attempt.MessageID, attempt.Phase, attempt.Ordinal, attempt.ClaimToken, reason,
	)
}

// Inspect는 행의 관측 상태를 읽기만 한다.
func (s *Store) Inspect(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyState, error) {
	var (
		state  irisdurable.ReplyState
		status string
	)

	err := s.db.QueryRow(ctx, queryInspectReply, s.opts.Scope, identity.MessageID, identity.Phase, identity.Ordinal).
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

// BackoffFor는 attempt번째 시도 뒤 다음 시도까지의 대기 시간이다. 호출자가 ReplyOutcome.RetryAfter에
// 그대로 넘겨 재시도 간격을 저장소 정책과 한 곳에서 맞출 수 있다.
func (s *Store) BackoffFor(attempt int) time.Duration {
	return time.Duration(s.backoffSeconds(attempt) * float64(time.Second))
}

func payloadHash(payload []byte) string {
	sum := sha256.Sum256(payload)

	return hex.EncodeToString(sum[:])
}
