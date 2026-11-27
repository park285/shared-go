package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/park285/shared-go/sqlite/sqlitedb"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

type InboxClaim struct {
	ID          int64
	MessageID   string
	OrderingKey string
	Payload     []byte
	ClaimToken  string
	Attempts    int
	CreatedAt   time.Time
	EligibleAt  time.Time
}

func (s *Store) Admit(ctx context.Context, input irisdurable.AdmissionInput) (workercontract.AdmissionResult, error) {
	if input.MessageID == "" || input.OrderingKey == "" || len(input.Payload) == 0 {
		return workercontract.AdmissionRejected, errors.New("sqlitestore: admission input requires messageID, orderingKey and payload")
	}

	accepted := false

	err := s.write(ctx, func(q querier, now int64) error {
		var id int64

		err := q.QueryRowContext(ctx, admitInboxSQL, s.opts.Scope, input.MessageID, input.OrderingKey, string(input.Payload), now).Scan(&id)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("insert inbox row: %w", err)
		}

		accepted = true

		return nil
	})
	if err != nil {
		return admissionFailure(err), fmt.Errorf("sqlitestore: admit %s: %w", input.MessageID, err)
	}

	if !accepted {
		return workercontract.AdmissionDuplicate, nil
	}

	return workercontract.AdmissionAccepted, nil
}

func admissionFailure(err error) workercontract.AdmissionResult {
	if errors.Is(err, sqlitedb.ErrCommit) {
		return workercontract.AdmissionOutcomeUnknown
	}

	if sqliteErr, ok := errors.AsType[*sqlite3.Error](err); ok && sqliteErr.Code() == sqlite3.CONSTRAINT {
		return workercontract.AdmissionRejected
	}

	return workercontract.AdmissionFailed
}

type ClaimBatchError struct {
	Token string
	Err   error
}

func (e *ClaimBatchError) Error() string { return e.Err.Error() }

func (e *ClaimBatchError) Unwrap() error { return e.Err }

func (s *Store) ClaimBatch(ctx context.Context, limit int) ([]InboxClaim, error) {
	if limit <= 0 {
		return nil, errors.New("sqlitestore: claim limit must be positive")
	}

	if s.opts.Lease <= 0 {
		return nil, errors.New("sqlitestore: claim inbox batch requires Options.Lease")
	}

	token, err := newClaimToken()
	if err != nil {
		return nil, err
	}

	var claims []InboxClaim

	err = s.write(ctx, func(q querier, now int64) error {
		claimed, claimErr := s.claimBatch(ctx, q, now, token, limit)

		claims = claimed

		return claimErr
	})
	if err != nil {
		return nil, &ClaimBatchError{Token: token, Err: fmt.Errorf("sqlitestore: claim inbox batch: %w", err)}
	}

	return claims, nil
}

type claimCandidate struct {
	id         int64
	eligibleAt int64
}

func (s *Store) claimBatch(ctx context.Context, q querier, now int64, token string, limit int) ([]InboxClaim, error) {
	candidates, err := s.claimCandidates(ctx, q, now, limit)
	if err != nil {
		return nil, err
	}

	claims := make([]InboxClaim, 0, len(candidates))
	leaseUntil := now + s.opts.Lease.Milliseconds()

	for _, candidate := range candidates {
		var (
			claim     InboxClaim
			payload   string
			createdMS int64
		)

		if err := q.QueryRowContext(ctx, claimInboxSQL, s.opts.Scope, candidate.id, token, leaseUntil, now).
			Scan(&claim.ID, &claim.MessageID, &claim.OrderingKey, &payload, &claim.ClaimToken, &claim.Attempts, &createdMS); err != nil {
			return nil, fmt.Errorf("claim inbox row %d: %w", candidate.id, err)
		}

		claim.Payload = []byte(payload)
		claim.CreatedAt = time.UnixMilli(createdMS).UTC()
		claim.EligibleAt = time.UnixMilli(candidate.eligibleAt).UTC()
		claims = append(claims, claim)
	}

	return claims, nil
}

func (s *Store) claimCandidates(ctx context.Context, q querier, now int64, limit int) ([]claimCandidate, error) {
	rows, err := q.QueryContext(ctx, claimCandidatesSQL, s.opts.Scope, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select inbox heads: %w", err)
	}
	defer rows.Close()

	var candidates []claimCandidate

	for rows.Next() {
		var candidate claimCandidate

		if err := rows.Scan(&candidate.id, &candidate.eligibleAt); err != nil {
			return nil, fmt.Errorf("scan inbox head: %w", err)
		}

		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read inbox heads: %w", err)
	}

	return candidates, nil
}

func (s *Store) ReleaseClaimToken(ctx context.Context, token string, keep []int64) (int64, error) {
	if token == "" {
		return 0, errors.New("sqlitestore: release claim token requires a token")
	}

	kept := []byte("[]")

	if len(keep) > 0 {
		encoded, err := json.Marshal(keep)
		if err != nil {
			return 0, fmt.Errorf("sqlitestore: encode kept claims: %w", err)
		}

		kept = encoded
	}

	var released int64

	err := s.write(ctx, func(q querier, now int64) error {
		affected, execErr := rowsAffected(ctx, q, "release claim token", releaseClaimTokenSQL, s.opts.Scope, token, string(kept), now)

		released = affected

		return execErr
	})
	if err != nil {
		return 0, err
	}

	return released, nil
}

func (s *Store) Complete(ctx context.Context, claim InboxClaim, reason string) error {
	return s.write(ctx, func(q querier, now int64) error {
		return execFenced(ctx, q, "complete inbox", completeInboxSQL, s.opts.Scope, claim.ID, claim.ClaimToken, reason, claim.MessageID, now)
	})
}

func (s *Store) ReleaseInboxAt(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.write(ctx, func(q querier, now int64) error {
		return execFenced(ctx, q, "release inbox at", releaseInboxAtSQL, s.opts.Scope, claim.ID, claim.ClaimToken, availableAt.UnixMilli(), now)
	})
}

func (s *Store) DeferInboxUntil(ctx context.Context, claim InboxClaim, availableAt time.Time) error {
	return s.write(ctx, func(q querier, now int64) error {
		return execFenced(ctx, q, "defer inbox until", deferInboxUntilSQL,
			s.opts.Scope, claim.ID, claim.ClaimToken, claim.MessageID, availableAt.UnixMilli(), now)
	})
}

func (s *Store) ManualReviewInbox(ctx context.Context, claim InboxClaim, reason string) error {
	if reason == "" {
		return errors.New("sqlitestore: manual review requires a reason")
	}

	return s.write(ctx, func(q querier, now int64) error {
		return execFenced(ctx, q, "manual review inbox", manualReviewInboxSQL,
			s.opts.Scope, claim.ID, claim.ClaimToken, reason, s.opts.DiscardInboxManualReviewPayload, now)
	})
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

func (decision *InboxRecoveryDecision) validate(index int) error {
	if decision.ID <= 0 || decision.MessageID == "" || decision.Attempts < 0 {
		return fmt.Errorf("sqlitestore: inbox recovery decision %d requires positive ID, messageID and nonnegative attempts", index)
	}

	if decision.ClaimToken != "" && decision.LeaseUntil.IsZero() {
		return fmt.Errorf("sqlitestore: inbox recovery decision %d requires a processing lease snapshot", index)
	}

	switch decision.Status {
	case "pending":
		if decision.Reason != "" {
			return fmt.Errorf("sqlitestore: inbox recovery decision %d pending reason must be empty", index)
		}
	case "completed":
	case "manual_review":
		if decision.Reason == "" {
			return fmt.Errorf("sqlitestore: inbox recovery decision %d manual review requires a reason", index)
		}
	default:
		return fmt.Errorf("sqlitestore: inbox recovery decision %d has an invalid target status", index)
	}

	return nil
}

func (s *Store) ApplyInboxRecovery(ctx context.Context, decisions []InboxRecoveryDecision) error {
	if s.tx == nil {
		return errors.New("sqlitestore: apply inbox recovery requires a transaction")
	}

	for i := range decisions {
		if err := decisions[i].validate(i); err != nil {
			return err
		}
	}

	now := time.Now().UnixMilli()

	for i := range decisions {
		decision := &decisions[i]

		var leaseUntil any

		if !decision.LeaseUntil.IsZero() {
			leaseUntil = decision.LeaseUntil.UnixMilli()
		}

		if err := execFenced(ctx, s.tx, "apply inbox recovery", applyInboxRecoverySQL, s.opts.Scope, decision.ID, decision.MessageID,
			decision.ClaimToken, decision.Attempts, decision.Status, decision.Reason, s.opts.DiscardInboxManualReviewPayload, now, leaseUntil); err != nil {
			return err
		}
	}

	return nil
}

type ReadySnapshot struct {
	Ready     int64
	OldestAge time.Duration
}

func (s *Store) InboxReadySnapshot(ctx context.Context) (ReadySnapshot, error) {
	var (
		snapshot ReadySnapshot
		oldest   sql.NullInt64
	)

	now := time.Now().UnixMilli()

	if err := s.read().QueryRowContext(ctx, inboxReadySnapshotSQL, s.opts.Scope, now).
		Scan(&snapshot.Ready, &oldest); err != nil {
		return ReadySnapshot{}, fmt.Errorf("sqlitestore: inbox ready snapshot: %w", err)
	}

	if oldest.Valid && oldest.Int64 < now {
		snapshot.OldestAge = time.Duration(now-oldest.Int64) * time.Millisecond
	}

	return snapshot, nil
}

func (s *Store) PruneInboxBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("sqlitestore: prune limit must be positive")
	}

	if s.opts.InboxTerminalRetention <= 0 {
		return 0, errors.New("sqlitestore: prune inbox before requires Options.InboxTerminalRetention")
	}

	var deleted int64

	err := s.write(ctx, func(q querier, now int64) error {
		completed, err := rowsAffected(ctx, q, "prune inbox completed", pruneInboxCompletedSQL,
			s.opts.Scope, s.opts.InboxTerminalRetention.Milliseconds(), limit, before.UnixMilli(), now)
		if err != nil {
			return err
		}

		deleted = completed

		remaining := int64(limit) - completed
		if s.opts.InboxManualReviewRetention <= 0 || remaining <= 0 {
			return nil
		}

		manual, err := rowsAffected(ctx, q, "prune inbox manual review", pruneInboxManualReviewSQL,
			s.opts.Scope, s.opts.InboxManualReviewRetention.Milliseconds(), remaining, before.UnixMilli(), now)

		deleted += manual

		return err
	})
	if err != nil {
		return 0, err
	}

	return deleted, nil
}
