package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// IsDuplicate는 최초 관측이면 키를 원자적으로 기록하고 false를, 이미 살아 있는 키면 true를 반환한다.
// 기록에 실패하면 오류를 반환해 fail-closed다.
func (s *Store) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if key == "" {
		return false, errors.New("pgstore: nonce key is required")
	}

	if ttl <= 0 {
		return false, errors.New("pgstore: nonce ttl must be positive")
	}

	tag, err := s.db.Exec(ctx, queryInsertNonce, s.opts.Scope, key, ttl.Seconds())
	if err != nil {
		return false, fmt.Errorf("pgstore: record nonce: %w", err)
	}

	return tag.RowsAffected() == 0, nil
}

// SetOnceNonce는 이 저장소가 set-once 성질을 보장한다는 마커다.
func (s *Store) SetOnceNonce() {}

// PruneNonce는 만료된 nonce 행을 지우고 지운 수를 반환한다.
func (s *Store) PruneNonce(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := s.db.Exec(ctx, queryPruneNonce, s.opts.Scope, limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune nonce: %w", err)
	}

	return tag.RowsAffected(), nil
}
