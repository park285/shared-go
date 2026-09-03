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
//
// 같은 ordering key의 삽입은 advisory transaction lock으로 직렬화한다. Querier가 pool이면
// 이 문의 암묵 트랜잭션이 끝날 때 lock이 풀리고, 호출자가 자기 트랜잭션을 넘겼으면 그
// 트랜잭션이 끝날 때까지 같은 key의 다른 Admit이 막힌다.
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

// Complete는 처리가 끝난 행을 종단으로 보내고 payload를 지운다. 인자 reason은 "재시도해도 같은
// 결과인 입력 결함"처럼 성공이 아닌 완료의 사유를 남기는 자리이고, 빈 값이면 기록하지 않는다.
//
// Fence는 claim token에 더해 message_id와 살아 있는 lease를 대조한다. 그 이유는
// queries/complete_inbox.sql이 적는다.
func (s *Store) Complete(ctx context.Context, claim InboxClaim, reason string) error {
	return s.execFenced(ctx, "complete inbox", queryCompleteInbox,
		s.opts.Scope, claim.ID, claim.ClaimToken, reason, claim.MessageID,
	)
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

// Defer는 처리를 시작하지 못한 행의 소유권만 반납하고 Claim이 올린 attempts를 되돌린다.
// 처리 결과가 아니므로 재시도 예산을 쓰지 않는 점이 Release와 다르다.
func (s *Store) Defer(ctx context.Context, claim InboxClaim, retryAfter time.Duration) error {
	if retryAfter < 0 {
		retryAfter = 0
	}

	return s.execFenced(ctx, "defer inbox", queryDeferInbox,
		s.opts.Scope, claim.ID, claim.ClaimToken, claim.MessageID, retryAfter.Seconds(),
	)
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

// PruneInbox는 보존이 지난 종단 행을 지우고 지운 수를 반환한다. 두 갈래의 보존은 따로이고,
// completed는 Options.InboxTerminalRetention을, manual_review는
// Options.InboxManualReviewRetention을 따른다. 후자가 0이면 사람이 처리할 때까지 남긴다.
func (s *Store) PruneInbox(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryPruneInbox,
		s.opts.Scope, s.opts.InboxTerminalRetention.Seconds(), s.opts.InboxManualReviewRetention.Seconds(), limit,
	)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune inbox: %w", err)
	}

	return tag.RowsAffected(), nil
}

// InboxRuntimeSnapshot은 inbox의 상태별 적재와 밀린 정도다.
type InboxRuntimeSnapshot struct {
	Pending      int64
	Processing   int64
	ManualReview int64
	// Due는 지금 손이 가야 하는 행 수다. pending은 available_at이, processing은 만료된 lease가 기준이다.
	Due int64
	// OldestDueAge는 가장 오래 밀린 행이 밀린 시간이다. 밀린 행이 없으면 0이다.
	OldestDueAge time.Duration
}

// ReadySnapshot은 지금 claim할 수 있는 행의 수와 가장 오래된 행의 나이다.
type ReadySnapshot struct {
	Ready     int64
	OldestAge time.Duration
}

// RuntimeSnapshot은 inbox 적재를 관측용으로 읽는다.
func (s *Store) RuntimeSnapshot(ctx context.Context) (InboxRuntimeSnapshot, error) {
	var (
		snapshot InboxRuntimeSnapshot
		ageSecs  float64
	)

	err := s.db.QueryRow(ctx, queryInboxRuntimeSnapshot, s.opts.Scope).
		Scan(&snapshot.Pending, &snapshot.Processing, &snapshot.ManualReview, &snapshot.Due, &ageSecs)
	if err != nil {
		return InboxRuntimeSnapshot{}, fmt.Errorf("pgstore: inbox runtime snapshot: %w", err)
	}

	snapshot.OldestDueAge = secondsToDuration(ageSecs)

	return snapshot, nil
}

// InboxReadySnapshot은 Claim이 지금 집을 수 있는 행을 센다. 술어는 claim_inbox.sql과 같다.
func (s *Store) InboxReadySnapshot(ctx context.Context) (ReadySnapshot, error) {
	return s.readySnapshot(ctx, "inbox ready snapshot", queryInboxReadySnapshot, s.opts.Scope)
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
