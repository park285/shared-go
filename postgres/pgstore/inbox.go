package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
		return workercontract.AdmissionRejected, errors.New("pgstore: admission input requires messageID, orderingKey and payload")
	}

	var id int64

	err := s.db.QueryRow(ctx, admitInboxSQL, s.opts.Scope, input.MessageID, input.OrderingKey, string(input.Payload)).Scan(&id)

	switch {
	case err == nil:
		return workercontract.AdmissionAccepted, nil
	case errors.Is(err, pgx.ErrNoRows):
		return workercontract.AdmissionDuplicate, nil
	}

	return admissionFailure(err), fmt.Errorf("pgstore: admit %s: %w", input.MessageID, err)
}

func admissionFailure(err error) workercontract.AdmissionResult {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return workercontract.AdmissionOutcomeUnknown
	}

	switch {
	case strings.HasPrefix(pgErr.Code, "22"), strings.HasPrefix(pgErr.Code, "23"):
		return workercontract.AdmissionRejected
	default:
		return workercontract.AdmissionFailed
	}
}

func (s *Store) Claim(ctx context.Context) (claim InboxClaim, ok bool, err error) {
	if policyErr := s.requirePolicy("claim inbox", true, false, false); policyErr != nil {
		return InboxClaim{}, false, policyErr
	}

	token, err := newClaimToken()
	if err != nil {
		return InboxClaim{}, false, err
	}

	var payload string

	row := s.db.QueryRow(ctx, claimInboxSQL, s.opts.Scope, token, s.opts.Lease.Seconds(), 1, s.opts.InboxExplicitRecovery)

	err = row.Scan(&claim.ID, &claim.MessageID, &claim.OrderingKey, &payload, &claim.ClaimToken, &claim.Attempts, &claim.CreatedAt, &claim.EligibleAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return InboxClaim{}, false, nil
	case err != nil:
		return InboxClaim{}, false, fmt.Errorf("pgstore: claim inbox: %w", err)
	}

	claim.Payload = []byte(payload)

	return claim, true, nil
}

func (s *Store) Complete(ctx context.Context, claim InboxClaim, reason string) error {
	return s.execFenced(ctx, "complete inbox", completeInboxSQL,
		s.opts.Scope, claim.ID, claim.ClaimToken, reason, claim.MessageID,
	)
}

func (s *Store) RenewInbox(ctx context.Context, claim InboxClaim) error {
	if err := s.requirePolicy("renew inbox lease", true, false, false); err != nil {
		return err
	}

	return s.execFenced(ctx, "renew inbox lease", renewInboxLeaseSQL, s.opts.Scope, claim.ID, claim.ClaimToken, s.opts.Lease.Seconds())
}

func (s *Store) Release(ctx context.Context, claim InboxClaim, retryAfter time.Duration) error {
	if retryAfter < 0 {
		retryAfter = 0
	}

	return s.execFenced(ctx, "release inbox", releaseInboxSQL, s.opts.Scope, claim.ID, claim.ClaimToken, retryAfter.Seconds(), nil)
}

func (s *Store) Defer(ctx context.Context, claim InboxClaim, retryAfter time.Duration) error {
	if retryAfter < 0 {
		retryAfter = 0
	}

	return s.execFenced(ctx, "defer inbox", deferInboxSQL,
		s.opts.Scope, claim.ID, claim.ClaimToken, claim.MessageID, retryAfter.Seconds(), nil,
	)
}

func (s *Store) ManualReviewInbox(ctx context.Context, claim InboxClaim, reason string) error {
	if reason == "" {
		return errors.New("pgstore: manual review requires a reason")
	}

	return s.execFenced(ctx, "manual review inbox", manualReviewInboxSQL,
		s.opts.Scope, claim.ID, claim.ClaimToken, reason, s.opts.DiscardInboxManualReviewPayload)
}

func (s *Store) PruneInbox(ctx context.Context, limit int) (int64, error) {
	return s.pruneInbox(ctx, "prune inbox", nil, limit)
}

type InboxRuntimeSnapshot struct {
	Pending      int64
	Processing   int64
	ManualReview int64
	Due          int64
	OldestDueAge time.Duration
}

type ReadySnapshot struct {
	Ready     int64
	OldestAge time.Duration
}

func (s *Store) RuntimeSnapshot(ctx context.Context) (InboxRuntimeSnapshot, error) {
	var (
		snapshot InboxRuntimeSnapshot
		ageSecs  float64
	)

	err := s.db.QueryRow(ctx, inboxRuntimeSnapshotSQL, s.opts.Scope).
		Scan(&snapshot.Pending, &snapshot.Processing, &snapshot.ManualReview, &snapshot.Due, &ageSecs)
	if err != nil {
		return InboxRuntimeSnapshot{}, fmt.Errorf("pgstore: inbox runtime snapshot: %w", err)
	}

	snapshot.OldestDueAge = secondsToDuration(ageSecs)

	return snapshot, nil
}

func (s *Store) InboxReadySnapshot(ctx context.Context) (ReadySnapshot, error) {
	return s.readySnapshot(ctx, "inbox ready snapshot", inboxReadySnapshotSQL, s.opts.Scope, s.opts.InboxExplicitRecovery)
}

func (s *Store) readySnapshot(ctx context.Context, action, sql string, args ...any) (ReadySnapshot, error) {
	var (
		snapshot ReadySnapshot
		ageSecs  float64
	)

	if err := s.db.QueryRow(ctx, sql, args...).Scan(&snapshot.Ready, &ageSecs); err != nil {
		return ReadySnapshot{}, fmt.Errorf("pgstore: %s: %w", action, err)
	}

	snapshot.OldestAge = secondsToDuration(ageSecs)

	return snapshot, nil
}

func secondsToDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}

	return time.Duration(seconds * float64(time.Second))
}

func (s *Store) execFenced(ctx context.Context, action, sql string, args ...any) error {
	tag, err := s.db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("pgstore: %s: %w", action, err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pgstore: %s: %w", action, ErrClaimLost)
	}

	return nil
}

func newClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("pgstore: generate claim token: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
