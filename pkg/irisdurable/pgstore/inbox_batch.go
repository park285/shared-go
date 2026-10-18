package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ClaimBatch는 서로 다른 ordering key의 준비된 head를 limit개까지 한 문장으로 claim한다.
// 반환 순서는 보장하지 않으며 같은 key의 후속 행은 앞 행이 종단이 될 때까지 반환하지 않는다.
// 조회 중 오류가 나면 이미 읽은 claim과 오류를 함께 반환한다. 누락 claim은 lease 복구가 소유한다.
func (s *Store) ClaimBatch(ctx context.Context, limit int) ([]InboxClaim, error) {
	if limit <= 0 {
		return nil, errors.New("pgstore: claim limit must be positive")
	}

	token, err := newClaimToken()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, queryClaimInbox, s.opts.Scope, token, s.opts.Lease.Seconds(), limit, s.opts.InboxExplicitRecovery)
	if err != nil {
		return nil, fmt.Errorf("pgstore: claim inbox batch: %w", err)
	}
	defer rows.Close()

	claims := make([]InboxClaim, 0, limit)

	for rows.Next() {
		var (
			claim   InboxClaim
			payload string
		)

		if err := rows.Scan(&claim.ID, &claim.MessageID, &claim.OrderingKey, &payload, &claim.ClaimToken, &claim.Attempts, &claim.CreatedAt, &claim.EligibleAt); err != nil {
			return claims, fmt.Errorf("pgstore: scan inbox batch: %w", err)
		}

		claim.Payload = []byte(payload)
		claims = append(claims, claim)
	}

	if err := rows.Err(); err != nil {
		return claims, fmt.Errorf("pgstore: read inbox batch: %w", err)
	}

	return claims, nil
}

// ReleaseInboxAt은 claim을 정확한 availableAt 이후의 재처리로 반환한다. 시도 횟수는 유지한다.
func (s *Store) ReleaseInboxAt(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.execFenced(ctx, "release inbox at", queryReleaseInbox,
		s.opts.Scope, claim.ID, claim.ClaimToken, float64(0), availableAt.UTC())
}

// DeferInboxUntil은 실행하지 않은 claim을 반환하고 시도 횟수를 되돌린다. AvailableAt을 보존한다.
func (s *Store) DeferInboxUntil(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.execFenced(ctx, "defer inbox until", queryDeferInbox,
		s.opts.Scope, claim.ID, claim.ClaimToken, claim.MessageID, float64(0), availableAt.UTC())
}

// InboxRecovery는 유한 lease 복구에서 자동 재처리와 수동 검토로 옮긴 행 수다.
type InboxRecovery struct {
	Retried      int64
	ManualReview int64
	// RetriedIDs와 ManualReviewIDs는 같은 transaction에서 소비자 metadata를 갱신할 행이다.
	RetriedIDs      []int64
	ManualReviewIDs []int64
}

// RecoverInboxBefore는 lease가 leaseBefore 전에 끝난 행을 limit개까지 복구한다.
// 아직 살아 있는 lease는 건드리지 않는다. MaxAttempts가 양수이면 소진 행은 manual_review가 된다.
// 본문 보존은 DiscardInboxManualReviewPayload를 따르며 scope와 row lock으로 다른 소유자를 격리한다.
func (s *Store) RecoverInboxBefore(ctx context.Context, leaseBefore time.Time, maxAttempts, limit int) (InboxRecovery, error) {
	if limit <= 0 {
		return InboxRecovery{}, errors.New("pgstore: recovery limit must be positive")
	}

	var result InboxRecovery

	if err := s.db.QueryRow(ctx, queryRecoverInboxBefore, s.opts.Scope, leaseBefore.UTC(), maxAttempts, limit,
		s.opts.DiscardInboxManualReviewPayload).Scan(&result.Retried, &result.ManualReview, &result.RetriedIDs, &result.ManualReviewIDs); err != nil {
		return InboxRecovery{}, fmt.Errorf("pgstore: recover inbox batch: %w", err)
	}

	return result, nil
}

// InboxRecoveryDecision은 잠근 inbox snapshot에 적용할 소비자의 복구 판정이다.
// ClaimToken이 비어 있으면 미청구 pending, 아니면 LeaseUntil까지 일치하는 processing만 바꾼다.
type InboxRecoveryDecision struct {
	ID         int64
	MessageID  string
	ClaimToken string
	Attempts   int
	LeaseUntil time.Time
	Status     string
	Reason     string
}

// ApplyInboxRecovery는 pending·completed·manual_review 판정을 한 문장으로 적용한다.
// Store는 호출자의 transaction에 묶여 있어야 한다. 호출자는 선택·업무 행을 잠그고 미청구·만료
// inbox 또는 자신이 소유한 처리 claim만 판정한다. 살아 있는 업무 lease는 소비자가 제외한다.
// 오류 시 업무 변경을 포함한 transaction 전체를 rollback한다.
// ErrClaimLost는 일부 행만 바뀐 경우에도 반환하므로 이 메서드의 오류를 무시하고 commit하면 안 된다.
func (s *Store) ApplyInboxRecovery(ctx context.Context, decisions []InboxRecoveryDecision) error {
	if _, ok := s.db.(txQuerier); !ok {
		return errors.New("pgstore: apply inbox recovery requires a transaction")
	}

	for i := range decisions {
		decision := &decisions[i]
		if decision.ID <= 0 || decision.MessageID == "" || decision.Attempts < 0 {
			return fmt.Errorf("pgstore: inbox recovery decision %d requires positive ID, messageID and nonnegative attempts", i)
		}

		if decision.ClaimToken != "" && decision.LeaseUntil.IsZero() {
			return fmt.Errorf("pgstore: inbox recovery decision %d requires a processing lease snapshot", i)
		}

		switch decision.Status {
		case "pending":
			if decision.Reason != "" {
				return fmt.Errorf("pgstore: inbox recovery decision %d pending reason must be empty", i)
			}
		case "completed":
		case "manual_review":
			if decision.Reason == "" {
				return fmt.Errorf("pgstore: inbox recovery decision %d manual review requires a reason", i)
			}
		default:
			return fmt.Errorf("pgstore: inbox recovery decision %d has an invalid target status", i)
		}
	}

	if len(decisions) == 0 {
		return nil
	}

	ids := make([]int64, len(decisions))
	messageIDs := make([]string, len(decisions))
	tokens := make([]string, len(decisions))
	attempts := make([]int, len(decisions))
	leases := make([]time.Time, len(decisions))
	statuses := make([]string, len(decisions))
	reasons := make([]string, len(decisions))

	for i := range decisions {
		decision := &decisions[i]

		ids[i] = decision.ID
		messageIDs[i] = decision.MessageID
		tokens[i] = decision.ClaimToken
		attempts[i] = decision.Attempts
		leases[i] = decision.LeaseUntil.UTC()
		statuses[i] = decision.Status
		reasons[i] = decision.Reason
	}

	tag, err := s.db.Exec(ctx, queryApplyInboxRecovery, s.opts.Scope,
		ids, messageIDs, tokens, attempts, leases, statuses, reasons, s.opts.DiscardInboxManualReviewPayload)
	if err != nil {
		return fmt.Errorf("pgstore: apply inbox recovery: %w", err)
	}

	if tag.RowsAffected() != int64(len(decisions)) {
		return fmt.Errorf("pgstore: apply inbox recovery: %w", ErrClaimLost)
	}

	return nil
}

// CompletePendingInboxBefore는 createdBefore보다 오래된 미청구 pending을 유한하게 종단 처리한다.
// 처리 중인 claim은 변경하지 않고 본문을 지운다. Reason은 성공과 만료 같은 종단 사유를 구분한다.
func (s *Store) CompletePendingInboxBefore(ctx context.Context, createdBefore time.Time, limit int, reason string) (int64, error) {
	if limit <= 0 || reason == "" {
		return 0, errors.New("pgstore: pending completion requires a positive limit and reason")
	}

	tag, err := s.db.Exec(ctx, queryCompleteInboxBefore, s.opts.Scope, createdBefore.UTC(), limit, reason)
	if err != nil {
		return 0, fmt.Errorf("pgstore: complete old pending inbox: %w", err)
	}

	return tag.RowsAffected(), nil
}

// PruneInboxBefore는 종단 시각이 before 이하인 행을 limit개까지 지운다.
// Before를 미래로 주더라도 Options의 최소 보존을 단축하지 않는다. 수동 검토 보존 0은 무기한이다.
func (s *Store) PruneInboxBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryPruneInbox, s.opts.Scope, s.opts.InboxTerminalRetention.Seconds(),
		s.opts.InboxManualReviewRetention.Seconds(), limit, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune inbox before: %w", err)
	}

	return tag.RowsAffected(), nil
}
