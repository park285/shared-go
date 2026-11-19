package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

type RedriveCandidate struct {
	irisdurable.ReplyIdentity

	RoomID          string
	ClientRequestID string
	Payload         []byte
	Attempts        int
}

func (s *Store) Redrive(ctx context.Context, limit int) ([]RedriveCandidate, error) {
	if limit <= 0 {
		return nil, errors.New("pgstore: redrive limit must be positive")
	}

	if err := s.requirePolicy("redrive replies", false, true, true); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, listRedrivableRepliesSQL, s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(), limit)
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

type RetiredReply struct {
	irisdurable.ReplyIdentity

	Status          irisdurable.ReplyStatus
	RoomID          string
	ClientRequestID string
	Payload         []byte
	Attempts        int
}

func (s *Store) Retire(ctx context.Context, limit int) ([]RetiredReply, error) {
	if _, ok := s.db.(txQuerier); !ok {
		return nil, errors.New("pgstore: retire requires a transaction")
	}

	if limit <= 0 {
		return nil, errors.New("pgstore: retire limit must be positive")
	}

	if err := s.requirePolicy("retire replies", false, true, true); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, retireExhaustedRepliesSQL, s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("pgstore: retire exhausted replies: %w", err)
	}
	defer rows.Close()

	retired := make([]RetiredReply, 0, limit)

	for rows.Next() {
		var (
			row     RetiredReply
			status  string
			payload *string
		)

		if err := rows.Scan(
			&row.MessageID, &row.Phase, &row.Ordinal, &status,
			&row.RoomID, &row.ClientRequestID, &payload, &row.Attempts,
		); err != nil {
			return nil, fmt.Errorf("pgstore: scan retired reply: %w", err)
		}

		row.Status = irisdurable.ReplyStatus(status)

		if payload != nil {
			row.Payload = []byte(*payload)
		}

		retired = append(retired, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: read retired replies: %w", err)
	}

	return retired, nil
}

func (s *Store) CountRepliesByStatus(ctx context.Context) (map[irisdurable.ReplyStatus]int64, error) {
	rows, err := s.db.Query(ctx, countRepliesByStatusSQL, s.opts.Scope)
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

func (s *Store) ReplyReadySnapshot(ctx context.Context) (ReadySnapshot, error) {
	if err := s.requirePolicy("reply ready snapshot", false, true, true); err != nil {
		return ReadySnapshot{}, err
	}

	return s.readySnapshot(ctx, "reply ready snapshot", replyReadySnapshotSQL,
		s.opts.Scope, s.opts.MaxAttempts, s.opts.AutomaticReplayHorizon.Seconds(),
	)
}

func (s *Store) PruneReplies(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, pruneRepliesSQL, s.opts.Scope, limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune replies: %w", err)
	}

	return tag.RowsAffected(), nil
}
