package sqlitestore

import (
	"context"
	"errors"
	"time"
)

func (s *Store) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if key == "" {
		return false, errors.New("sqlitestore: nonce key is required")
	}

	if ttl <= 0 {
		return false, errors.New("sqlitestore: nonce ttl must be positive")
	}

	recorded := int64(0)

	err := s.write(ctx, func(q querier, now int64) error {
		affected, err := rowsAffected(ctx, q, "record nonce", insertNonceSQL, s.opts.Scope, key, max(ttl.Milliseconds(), 1), now)

		recorded = affected

		return err
	})
	if err != nil {
		return false, err
	}

	return recorded == 0, nil
}

func (s *Store) SetOnceNonce() {}

func (s *Store) PruneNonce(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("sqlitestore: prune limit must be positive")
	}

	var pruned int64

	err := s.write(ctx, func(q querier, now int64) error {
		affected, err := rowsAffected(ctx, q, "prune nonce", pruneNonceSQL, s.opts.Scope, limit, now)

		pruned = affected

		return err
	})
	if err != nil {
		return 0, err
	}

	return pruned, nil
}
