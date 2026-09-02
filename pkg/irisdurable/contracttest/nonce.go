package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

const maxNonceExpiry = 5 * time.Second

func runNonce(t *testing.T, newStore func(*testing.T) irisdurable.NonceStore, expiry time.Duration) {
	t.Helper()

	t.Run("FirstObservationRecordsOnce", func(t *testing.T) {
		store := newStore(t)
		store.SetOnceNonce()

		key := uniqueID("nonce")

		requireDuplicate(t, store, key, time.Hour, false)
		requireDuplicate(t, store, key, time.Hour, true)
		requireDuplicate(t, store, uniqueID("nonce"), time.Hour, false)
	})

	t.Run("CanceledContextFailsClosed", func(t *testing.T) {
		store := newStore(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if _, err := store.IsDuplicate(ctx, uniqueID("nonce"), time.Hour); err == nil {
			t.Fatal("IsDuplicate with canceled context must fail closed")
		}
	})

	if expiry <= 0 {
		return
	}

	if expiry > maxNonceExpiry {
		t.Fatalf("NonceExpiry %s exceeds the %s bound", expiry, maxNonceExpiry)
	}

	t.Run("ExpiredKeyIsObservedAgain", func(t *testing.T) {
		store := newStore(t)
		key := uniqueID("nonce")

		requireDuplicate(t, store, key, expiry, false)
		time.Sleep(expiry + expiry/2)
		requireDuplicate(t, store, key, expiry, false)
	})
}

func requireDuplicate(t *testing.T, store irisdurable.NonceStore, key string, ttl time.Duration, want bool) {
	t.Helper()

	got, err := store.IsDuplicate(t.Context(), key, ttl)
	if err != nil {
		t.Fatalf("IsDuplicate(%s): %v", key, err)
	}

	if got != want {
		t.Fatalf("IsDuplicate(%s) = %v; want %v", key, got, want)
	}
}
