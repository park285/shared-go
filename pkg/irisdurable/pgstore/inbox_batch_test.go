package pgstore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestInboxBatchKeepsFIFOAndRequiresBoundedRecovery(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope: "chat", InboxExplicitRecovery: true, DiscardInboxManualReviewPayload: true,
	})

	claims := claimFIFOHeads(t, store)

	if _, err := pool.Exec(t.Context(), "UPDATE iris_webhook_inbox SET lease_until = now() - interval '1 hour' WHERE status = 'processing'"); err != nil {
		t.Fatal(err)
	}

	if ready, err := store.InboxReadySnapshot(t.Context()); err != nil || ready.Ready != 0 {
		t.Fatalf("ready before explicit recovery = %+v, %v", ready, err)
	}

	if rows, err := store.ClaimBatch(t.Context(), 8); err != nil || len(rows) != 0 {
		t.Fatalf("claim reclaimed without recovery: %+v, %v", rows, err)
	}

	result, err := store.RecoverInboxBefore(t.Context(), time.Now(), 1, 1)
	if err != nil || result.ManualReview != 1 || result.Retried != 0 {
		t.Fatalf("bounded recovery = %+v, %v", result, err)
	}

	var processing, scrubbed int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FILTER (WHERE status = 'processing'), count(*) FILTER (WHERE status = 'manual_review' AND payload = '{}'::jsonb) FROM iris_webhook_inbox").Scan(&processing, &scrubbed); err != nil {
		t.Fatal(err)
	}

	if processing != 1 || scrubbed != 1 {
		t.Fatalf("processing=%d scrubbed=%d", processing, scrubbed)
	}

	for _, claim := range claims {
		if err := store.Complete(t.Context(), claim, ""); !errors.Is(err, pgstore.ErrClaimLost) {
			t.Fatalf("expired owner completed: %v", err)
		}
	}
}

func TestInboxExactRetryAndUnattemptedFence(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)

	if _, err := store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "first", OrderingKey: "one", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	first, ok, claimErr := store.Claim(t.Context())
	if claimErr != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, claimErr)
	}

	due := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	if err := store.DeferInboxUntil(t.Context(), first, due); err != nil {
		t.Fatal(err)
	}

	var (
		stored   time.Time
		attempts int
	)

	if err := pool.QueryRow(t.Context(), "SELECT available_at, attempts FROM iris_webhook_inbox WHERE id = $1", first.ID).Scan(&stored, &attempts); err != nil {
		t.Fatal(err)
	}

	if !stored.Equal(due) || attempts != 0 {
		t.Fatalf("defer stored=%v attempts=%d", stored, attempts)
	}

	second, ok, err := store.Claim(t.Context())
	if err != nil || !ok || second.Attempts != 1 || second.ClaimToken == first.ClaimToken {
		t.Fatalf("second claim=%+v ok=%v err=%v", second, ok, err)
	}

	if err := store.ReleaseInboxAt(t.Context(), first, due); !errors.Is(err, pgstore.ErrClaimLost) {
		t.Fatalf("old token updated equal-attempt row: %v", err)
	}

	if err := store.ReleaseInboxAt(t.Context(), second, due); err != nil {
		t.Fatal(err)
	}

	if err := pool.QueryRow(t.Context(), "SELECT available_at, attempts FROM iris_webhook_inbox WHERE id = $1", first.ID).Scan(&stored, &attempts); err != nil {
		t.Fatal(err)
	}

	if !stored.Equal(due) || attempts != 1 {
		t.Fatalf("retry stored=%v attempts=%d", stored, attempts)
	}
}

func TestInboxTerminalMaintenancePreservesRetentionAndClaim(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope: "chat", InboxManualReviewRetention: 7 * 24 * time.Hour,
		DiscardInboxManualReviewPayload: true,
	})

	for _, id := range []string{"claimed", "stale-one", "stale-two"} {
		if _, err := store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: id, OrderingKey: id, Payload: []byte(`{"private":"body"}`)}); err != nil {
			t.Fatal(err)
		}
	}

	claim, ok, err := store.Claim(t.Context())
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}

	if _, err := pool.Exec(t.Context(), "UPDATE iris_webhook_inbox SET created_at = now() - interval '1 day'"); err != nil {
		t.Fatal(err)
	}

	if n, err := store.CompletePendingInboxBefore(t.Context(), time.Now(), 1, "inbox age exceeded"); err != nil || n != 1 {
		t.Fatalf("stale sweep = %d, %v", n, err)
	}

	if err := store.ManualReviewInbox(t.Context(), claim, "permanent failure"); err != nil {
		t.Fatalf("sweep stole active claim: %v", err)
	}

	if n, err := store.PruneInboxBefore(t.Context(), time.Now().Add(time.Hour), 10); err != nil || n != 0 {
		t.Fatalf("future cutoff bypassed retention = %d, %v", n, err)
	}

	if _, err := pool.Exec(t.Context(), "UPDATE iris_webhook_inbox SET terminal_at = now() - interval '8 days' WHERE status IN ('completed', 'manual_review')"); err != nil {
		t.Fatal(err)
	}

	if n, err := store.PruneInboxBefore(t.Context(), time.Now(), 1); err != nil || n != 1 {
		t.Fatalf("bounded prune = %d, %v", n, err)
	}

	var scrubbed int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM iris_webhook_inbox WHERE status IN ('completed', 'manual_review') AND payload = '{}'::jsonb").Scan(&scrubbed); err != nil {
		t.Fatal(err)
	}

	if scrubbed != 1 {
		t.Fatalf("remaining scrubbed terminal = %d", scrubbed)
	}
}

func claimFIFOHeads(t *testing.T, store *pgstore.Store) []pgstore.InboxClaim {
	t.Helper()

	for _, input := range []irisdurable.AdmissionInput{
		{MessageID: "first", OrderingKey: "one", Payload: []byte(`{"text":"one"}`)},
		{MessageID: "second", OrderingKey: "one", Payload: []byte(`{"text":"two"}`)},
		{MessageID: "other", OrderingKey: "two", Payload: []byte(`{"text":"other"}`)},
	} {
		if got, err := store.Admit(t.Context(), input); err != nil || got != workercontract.AdmissionAccepted {
			t.Fatalf("admit %s = %s, %v", input.MessageID, got, err)
		}
	}

	claims, err := store.ClaimBatch(t.Context(), 8)
	if err != nil || len(claims) != 2 {
		t.Fatalf("claim = %v, %v", claims, err)
	}

	for _, claim := range claims {
		if claim.MessageID == "second" || claim.CreatedAt.IsZero() || claim.Attempts != 1 {
			t.Fatalf("invalid head claim: %+v", claim)
		}
	}

	return claims
}
