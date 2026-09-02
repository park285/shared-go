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
