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

// InboxClaim은 처리 소유권을 잡은 inbox 행이다. ClaimToken은 Complete·Release의 fence다.
type InboxClaim struct {
	ID          int64
	MessageID   string
	OrderingKey string
	Payload     []byte
	ClaimToken  string
	Attempts    int
}

// Admit은 HTTP 200 전에 메시지를 inbox에 commit한다.
func (s *Store) Admit(ctx context.Context, input irisdurable.AdmissionInput) (workercontract.AdmissionResult, error) {
	if input.MessageID == "" || input.OrderingKey == "" || len(input.Payload) == 0 {
		return workercontract.AdmissionRejected, errors.New("pgstore: admission input requires messageID, orderingKey and payload")
	}

	var id int64

	err := s.db.QueryRow(ctx, queryAdmitInbox, s.opts.Scope, input.MessageID, input.OrderingKey, string(input.Payload)).Scan(&id)

	switch {
	case err == nil:
		return workercontract.AdmissionAccepted, nil
	case errors.Is(err, pgx.ErrNoRows):
		return workercontract.AdmissionDuplicate, nil
	}

	return admissionFailure(err), fmt.Errorf("pgstore: admit %s: %w", input.MessageID, err)
}

// admissionFailure는 실패한 admit의 commit 여부를 판정한다.
//
// 서버가 SQLSTATE를 돌려줬다면 단일 문의 암묵 트랜잭션이 rollback된 것이므로 commit되지 않은 것이
// 확실하다. 그중 22(data exception)와 23(integrity constraint violation)은 같은 payload를 다시 보내도
// 같은 결과라 재전송이 무의미하므로 Rejected이고, 나머지 서버 오류는 재전송이 성공할 수 있어
// Failed다. 응답 자체가 없으면(네트워크 단절·취소·타임아웃) commit 여부를 알 수 없으므로
// OutcomeUnknown이고, 호출자가 503으로 매핑해 Iris 재전송을 유도한다.
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

// Claim은 처리 가능한 다음 메시지의 소유권을 잡는다. 후보가 없으면 ok가 false다.
//
// 같은 ordering key에 더 오래된 미종단 행이 있으면 그 행이 먼저 끝나야 하므로 FIFO가 유지된다.
func (s *Store) Claim(ctx context.Context) (claim InboxClaim, ok bool, err error) {
	token, err := newClaimToken()
	if err != nil {
		return InboxClaim{}, false, err
	}

	var payload string

	row := s.db.QueryRow(ctx, queryClaimInbox, s.opts.Scope, token, s.opts.Lease.Seconds())

	err = row.Scan(&claim.ID, &claim.MessageID, &claim.OrderingKey, &payload, &claim.ClaimToken, &claim.Attempts)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return InboxClaim{}, false, nil
	case err != nil:
		return InboxClaim{}, false, fmt.Errorf("pgstore: claim inbox: %w", err)
	}

	claim.Payload = []byte(payload)

	return claim, true, nil
}

// Complete는 처리가 끝난 행을 종단으로 보내고 payload를 지운다.
func (s *Store) Complete(ctx context.Context, claim InboxClaim) error {
	return s.execFenced(ctx, "complete inbox", queryCompleteInbox, s.opts.Scope, claim.ID, claim.ClaimToken)
}

// RenewInbox는 처리 중인 행의 lease를 Options.Lease만큼 연장한다. 처리 시간이 lease보다 긴
// 호출자가 heartbeat로 소유권을 유지하는 경로다. 이미 lease를 잃었으면 ErrClaimLost다.
func (s *Store) RenewInbox(ctx context.Context, claim InboxClaim) error {
	return s.execFenced(ctx, "renew inbox lease", queryRenewInboxLease, s.opts.Scope, claim.ID, claim.ClaimToken, s.opts.Lease.Seconds())
}

// Release는 처리에 실패한 행을 retryAfter 뒤 다시 처리하도록 되돌린다. 표준 지수 백오프는
// 호출자가 BackoffFor(claim.Attempts)로 얻고, 0은 즉시 재처리를 뜻한다.
func (s *Store) Release(ctx context.Context, claim InboxClaim, retryAfter time.Duration) error {
	if retryAfter < 0 {
		retryAfter = 0
	}

	return s.execFenced(ctx, "release inbox", queryReleaseInbox, s.opts.Scope, claim.ID, claim.ClaimToken, retryAfter.Seconds())
}

// ManualReviewInbox는 자동으로 판정할 수 없는 행을 사람이 볼 안전 경계 상태로 보낸다.
// 운영자가 원인을 볼 수 있도록 payload는 남긴다.
func (s *Store) ManualReviewInbox(ctx context.Context, claim InboxClaim, reason string) error {
	if reason == "" {
		return errors.New("pgstore: manual review requires a reason")
	}

	return s.execFenced(ctx, "manual review inbox", queryManualReviewInbox, s.opts.Scope, claim.ID, claim.ClaimToken, reason)
}

// ReclaimInbox는 lease가 끊긴 행을 다시 처리 가능하게 되돌리고 그 수를 반환한다.
func (s *Store) ReclaimInbox(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, queryReclaimInbox, s.opts.Scope)
	if err != nil {
		return 0, fmt.Errorf("pgstore: reclaim inbox: %w", err)
	}

	return tag.RowsAffected(), nil
}

// PruneInbox는 보존이 지난 completed 행을 지우고 지운 수를 반환한다.
// 사람이 처리할 때까지 manual_review 행은 남긴다.
func (s *Store) PruneInbox(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryPruneInbox, s.opts.Scope, s.opts.InboxTerminalRetention.Seconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune inbox: %w", err)
	}

	return tag.RowsAffected(), nil
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
