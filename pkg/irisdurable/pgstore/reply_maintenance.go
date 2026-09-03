package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// RedriveCandidate는 다시 발송을 시도할 수 있는 outbox 행이다.
type RedriveCandidate struct {
	irisdurable.ReplyIdentity

	RoomID          string
	ClientRequestID string
	Payload         []byte
	Attempts        int
}

// Redrive는 저장된 clientRequestId로 다시 보낼 수 있는 행을 자동 replay 지평 안에서만 돌려준다.
// 소유권은 잡지 않으므로 호출자가 BeginAttempt로 fence를 얻어야 한다.
func (s *Store) Redrive(ctx context.Context, limit int) ([]RedriveCandidate, error) {
	if limit <= 0 {
		return nil, errors.New("pgstore: redrive limit must be positive")
	}

	rows, err := s.db.Query(ctx, queryListRedrivableReplies, s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("pgstore: list redrivable replies: %w", err)
	}
	defer rows.Close()

	candidates := make([]RedriveCandidate, 0, limit)

	for rows.Next() {
		var (
			candidate RedriveCandidate
			payload   string
		)

		if err := rows.Scan(
			&candidate.MessageID, &candidate.Phase, &candidate.Ordinal,
			&candidate.RoomID, &candidate.ClientRequestID, &payload, &candidate.Attempts,
		); err != nil {
			return nil, fmt.Errorf("pgstore: scan redrivable reply: %w", err)
		}

		candidate.Payload = []byte(payload)
		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: read redrivable replies: %w", err)
	}

	return candidates, nil
}

// Retire는 더 보낼 수 없는 행을 dead로 보내고 payload를 지운 뒤 그 수를 반환한다.
func (s *Store) Retire(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: retire limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryRetireExhaustedReplies, s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: retire exhausted replies: %w", err)
	}

	return tag.RowsAffected(), nil
}

// CountRepliesByStatus는 scope의 outbox 행을 상태별로 센다. 어느 상태에도 행이 없으면 그 키는
// 결과에 없다.
func (s *Store) CountRepliesByStatus(ctx context.Context) (map[irisdurable.ReplyStatus]int64, error) {
	rows, err := s.db.Query(ctx, queryCountRepliesByStatus, s.opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("pgstore: count replies by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[irisdurable.ReplyStatus]int64)

	for rows.Next() {
		var (
			status string
			count  int64
		)

		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("pgstore: scan reply status count: %w", err)
		}

		counts[irisdurable.ReplyStatus(status)] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: read reply status counts: %w", err)
	}

	return counts, nil
}

// ReplyReadySnapshot은 Redrive가 지금 돌려줄 수 있는 행을 센다. 술어는
// list_redrivable_replies.sql과 같다.
func (s *Store) ReplyReadySnapshot(ctx context.Context) (ReadySnapshot, error) {
	return s.readySnapshot(ctx, "reply ready snapshot", queryReplyReadySnapshot,
		s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(),
	)
}

// PruneReplies는 보존이 지난 행을 지우고 지운 수를 반환한다.
// 사람이 처리할 때까지 manual_review 행은 남긴다.
func (s *Store) PruneReplies(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryPruneReplies, s.opts.Scope, limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune replies: %w", err)
	}

	return tag.RowsAffected(), nil
}
