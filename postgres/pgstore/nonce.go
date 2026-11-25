package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
)

func (s *Store) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if key == "" {
		return false, errors.New("pgstore: nonce key is required")
	}

	if ttl <= 0 {
		return false, errors.New("pgstore: nonce ttl must be positive")
	}

	tag, err := pgxdb.Exec(ctx, s.db, insertNonceSQL, s.opts.Scope, key, ttl.Seconds())
	if err != nil {
		return false, fmt.Errorf("pgstore: record nonce: %w", err)
	}

	return tag.RowsAffected() == 0, nil
}

func (s *Store) SetOnceNonce() {}

func (s *Store) PruneNonce(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("pgstore: prune limit must be positive")
	}

	tag, err := pgxdb.Exec(ctx, s.db, pruneNonceSQL, s.opts.Scope, limit)
	if err != nil {
		return 0, fmt.Errorf("pgstore: prune nonce: %w", err)
	}

	return tag.RowsAffected(), nil
}
