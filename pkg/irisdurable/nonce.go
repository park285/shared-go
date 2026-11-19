package irisdurable

import (
	"context"
	"time"
)

type NonceStore interface {
	IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error)
	SetOnceNonce()
}
