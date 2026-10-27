package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ClaimBatch는 서로 다른 ordering key의 준비된 head를 limit개까지 한 문장으로 claim한다.
// 반환 순서는 보장하지 않으며 같은 key의 후속 행은 앞 행이 종단이 될 때까지 반환하지 않는다.
//
// 문장을 보낸 뒤의 claim 오류는 *ClaimBatchError로 감싸 이미 읽은 claim과 함께 반환한다. 이때 일부 행은
// 반환되지 않은 채 commit됐을 수 있으므로, 호출자는 반환된 claim을 처리하고 나머지를 ClaimBatchError.Token으로
// ReleaseClaimToken에 돌려준다. 반납하지 않은 claim은 lease 만료까지 같은 ordering key를 막는다.
func (s *Store) ClaimBatch(ctx context.Context, limit int) ([]InboxClaim, error) {
	if limit <= 0 {
		return nil, errors.New("pgstore: claim limit must be positive")
	}

	if err := s.requirePolicy("claim inbox batch", true, false, false); err != nil {
		return nil, err
	}

	token, err := newClaimToken()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, queryClaimInbox, s.opts.Scope, token, s.opts.Lease.Seconds(), limit, s.opts.InboxExplicitRecovery)
	if err != nil {
		return nil, &ClaimBatchError{Token: token, Err: fmt.Errorf("pgstore: claim inbox batch: %w", err)}
	}
	defer rows.Close()

	claims := make([]InboxClaim, 0, limit)

	for rows.Next() {
		var (
			claim   InboxClaim
			payload string
		)

		if err := rows.Scan(&claim.ID, &claim.MessageID, &claim.OrderingKey, &payload, &claim.ClaimToken, &claim.Attempts, &claim.CreatedAt, &claim.EligibleAt); err != nil {
			return claims, &ClaimBatchError{Token: token, Err: fmt.Errorf("pgstore: scan inbox batch: %w", err)}
		}

		claim.Payload = []byte(payload)
		claims = append(claims, claim)
	}

	if err := rows.Err(); err != nil {
		return claims, &ClaimBatchError{Token: token, Err: fmt.Errorf("pgstore: read inbox batch: %w", err)}
	}

	return claims, nil
}

// ClaimBatchError는 ClaimBatch가 claim 문을 보낸 뒤 결과를 끝까지 읽지 못한 오류다. 행이 commit됐는지
// 알 수 없으므로 결과는 미상이며, Token은 이번 호출이 쓴 claim token이다.
type ClaimBatchError struct {
	Token string
	Err   error
}

func (e *ClaimBatchError) Error() string { return e.Err.Error() }

func (e *ClaimBatchError) Unwrap() error { return e.Err }

// ReleaseClaimToken은 token이 아직 소유한 processing 행을 모두 pending으로 되돌리고 Claim이 올린
// attempts를 원복한 뒤 되돌린 행 수를 반환한다. 처리를 시작하지 않은 claim의 반납이라 available_at은
// 그대로 두며, 이미 종단이거나 다른 token으로 넘어간 행은 바꾸지 않는다. 호출자가 소유를 잃은 claim을
// 같은 호출 안에서 반납하도록 종료 취소와 분리된 context(context.WithoutCancel과 짧은 기한)로 부른다.
func (s *Store) ReleaseClaimToken(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, errors.New("pgstore: release claim token requires a token")
	}

	tag, err := s.db.Exec(ctx, queryReleaseClaimToken, s.opts.Scope, token)
	if err != nil {
		return 0, fmt.Errorf("pgstore: release claim token: %w", err)
	}

	return tag.RowsAffected(), nil
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

// PruneInboxBefore는 종단 시각이 before 이하인 행을 limit개까지 지운다.
// Before를 미래로 주더라도 Options의 최소 보존을 단축하지 않는다. 수동 검토 보존 0은 무기한이다.
func (s *Store) PruneInboxBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return s.pruneInbox(ctx, "prune inbox before", before.UTC(), limit)
}

// pruneInbox는 completed와 manual_review 갈래를 각자의 부분 인덱스 순서로 끝나는 별도 문장으로 지운다.
// 한 문장의 OR 술어는 두 인덱스 순서를 쓰지 못해 기한이 지난 행 전체를 읽고 정렬하므로 나눈다.
// 수동 검토 갈래는 보존이 지정됐을 때만 실행하고 앞 갈래가 지운 수를 뺀 나머지 한도만 써서
// 한 호출이 limit보다 많이 지우지 않는다. 인자 before가 nil이면 보존 기준만 쓴다.
func (s *Store) pruneInbox(ctx context.Context, action string, before any, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	if s.opts.InboxTerminalRetention <= 0 {
		return 0, fmt.Errorf("pgstore: %s requires Options.InboxTerminalRetention", action)
	}

	tag, err := s.db.Exec(ctx, queryPruneInboxCompleted, s.opts.Scope, s.opts.InboxTerminalRetention.Seconds(), limit, before)
	if err != nil {
		return 0, fmt.Errorf("pgstore: %s completed: %w", action, err)
	}

	deleted := tag.RowsAffected()

	remaining := int64(limit) - deleted
	if s.opts.InboxManualReviewRetention <= 0 || remaining <= 0 {
		return deleted, nil
	}

	tag, err = s.db.Exec(ctx, queryPruneInboxManualReview, s.opts.Scope, s.opts.InboxManualReviewRetention.Seconds(), remaining, before)
	if err != nil {
		return deleted, fmt.Errorf("pgstore: %s manual review: %w", action, err)
	}

	return deleted + tag.RowsAffected(), nil
}
