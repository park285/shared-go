package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

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

	rows, err := s.db.Query(ctx, claimInboxSQL, s.opts.Scope, token, s.opts.Lease.Seconds(), limit, s.opts.InboxExplicitRecovery)
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

type ClaimBatchError struct {
	Token string
	Err   error
}

func (e *ClaimBatchError) Error() string { return e.Err.Error() }

func (e *ClaimBatchError) Unwrap() error { return e.Err }

func (s *Store) ReleaseClaimToken(ctx context.Context, token string, keep []int64) (int64, error) {
	if token == "" {
		return 0, errors.New("pgstore: release claim token requires a token")
	}

	tag, err := s.db.Exec(ctx, releaseClaimTokenSQL, s.opts.Scope, token, keep)
	if err != nil {
		return 0, fmt.Errorf("pgstore: release claim token: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (s *Store) ReleaseInboxAt(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.execFenced(ctx, "release inbox at", releaseInboxSQL,
		s.opts.Scope, claim.ID, claim.ClaimToken, float64(0), availableAt.UTC())
}

func (s *Store) DeferInboxUntil(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.execFenced(ctx, "defer inbox until", deferInboxSQL,
		s.opts.Scope, claim.ID, claim.ClaimToken, claim.MessageID, float64(0), availableAt.UTC())
}

type InboxRecoveryDecision struct {
	ID         int64
	MessageID  string
	ClaimToken string
	Attempts   int
	LeaseUntil time.Time
	Status     string
	Reason     string
}

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

	tag, err := s.db.Exec(ctx, applyInboxRecoverySQL, s.opts.Scope,
		ids, messageIDs, tokens, attempts, leases, statuses, reasons, s.opts.DiscardInboxManualReviewPayload)
	if err != nil {
		return fmt.Errorf("pgstore: apply inbox recovery: %w", err)
	}

	if tag.RowsAffected() != int64(len(decisions)) {
		return fmt.Errorf("pgstore: apply inbox recovery: %w", ErrClaimLost)
	}

	return nil
}

func (s *Store) PruneInboxBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	return s.pruneInbox(ctx, "prune inbox before", before.UTC(), limit)
}

func (s *Store) pruneInbox(ctx context.Context, action string, before any, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	if s.opts.InboxTerminalRetention <= 0 {
		return 0, fmt.Errorf("pgstore: %s requires Options.InboxTerminalRetention", action)
	}

	tag, err := s.db.Exec(ctx, pruneInboxCompletedSQL, s.opts.Scope, s.opts.InboxTerminalRetention.Seconds(), limit, before)
	if err != nil {
		return 0, fmt.Errorf("pgstore: %s completed: %w", action, err)
	}

	deleted := tag.RowsAffected()

	remaining := int64(limit) - deleted
	if s.opts.InboxManualReviewRetention <= 0 || remaining <= 0 {
		return deleted, nil
	}

	tag, err = s.db.Exec(ctx, pruneInboxManualReviewSQL, s.opts.Scope, s.opts.InboxManualReviewRetention.Seconds(), remaining, before)
	if err != nil {
		return deleted, fmt.Errorf("pgstore: %s manual review: %w", action, err)
	}

	return deleted + tag.RowsAffected(), nil
}
