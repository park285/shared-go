package pgstore_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const (
	inboxCompletedStatus = "completed"
	inboxRecoveryScope   = "recovery"
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

	for index := range claims {
		claim := &claims[index]
		if claim.MessageID == "second" || claim.CreatedAt.IsZero() || claim.Attempts != 1 {
			t.Fatalf("invalid head claim: %+v", claim)
		}
	}

	return claims
}

func TestApplyInboxRecoveryFencesSnapshots(t *testing.T) {
	pool := newMigratedPool(t)
	tests := []struct {
		name       string
		pending    bool
		otherScope bool
		change     func(*pgstore.InboxRecoveryDecision)
	}{
		{name: "ID", change: func(d *pgstore.InboxRecoveryDecision) { d.ID += 1000000 }},
		{name: "message", change: func(d *pgstore.InboxRecoveryDecision) { d.MessageID += "-replacement" }},
		{name: "attempts", change: func(d *pgstore.InboxRecoveryDecision) { d.Attempts++ }},
		{name: "token", change: func(d *pgstore.InboxRecoveryDecision) { d.ClaimToken += "-stale" }},
		{name: "renewed lease", change: func(d *pgstore.InboxRecoveryDecision) { d.LeaseUntil = d.LeaseUntil.Add(-time.Microsecond) }},
		{name: "pending snapshot cannot replace processing", change: func(d *pgstore.InboxRecoveryDecision) { d.ClaimToken = ""; d.LeaseUntil = time.Time{} }},
		{name: "processing snapshot cannot replace pending", pending: true, change: func(d *pgstore.InboxRecoveryDecision) { d.ClaimToken = "stale"; d.LeaseUntil = time.Now() }},
		{name: "scope", otherScope: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := seedInboxRecovery(t, pool, tt.name, tt.pending)
			before := inboxRecoverySnapshot(t, pool, decision.ID)
			id := decision.ID
			opts := pgstore.Options{Scope: inboxRecoveryScope}

			if tt.otherScope {
				opts.Scope = "another-owner"
			}

			if tt.change != nil {
				tt.change(&decision)
			}

			store, tx := inboxRecoveryTransaction(t, pool, opts)
			require.ErrorIs(t, store.ApplyInboxRecovery(t.Context(), []pgstore.InboxRecoveryDecision{decision}), pgstore.ErrClaimLost)
			require.Equal(t, before, inboxRecoverySnapshot(t, tx, id), "lost snapshot changed the row")
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
		})
	}
}

func TestApplyInboxRecoveryRollbackRestoresPartialBatch(t *testing.T) {
	pool := newMigratedPool(t)

	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%t", duplicate), func(t *testing.T) {
			first := seedInboxRecovery(t, pool, t.Name()+"-first", false)
			second := seedInboxRecovery(t, pool, t.Name()+"-second", true)
			beforeFirst := inboxRecoverySnapshot(t, pool, first.ID)
			beforeSecond := inboxRecoverySnapshot(t, pool, second.ID)
			decisions := []pgstore.InboxRecoveryDecision{first, second}

			if duplicate {
				decisions[1] = first
			} else {
				decisions[1].MessageID += "-stale"
			}

			store, tx := inboxRecoveryTransaction(t, pool, pgstore.Options{Scope: inboxRecoveryScope})
			require.ErrorIs(t, store.ApplyInboxRecovery(t.Context(), decisions), pgstore.ErrClaimLost)

			var status string

			require.NoError(t, tx.QueryRow(t.Context(), "SELECT status FROM iris_webhook_inbox WHERE id = $1", first.ID).Scan(&status))
			require.Equal(t, inboxCompletedStatus, status, "matching rows change before caller rollback")
			require.Equal(t, beforeSecond, inboxRecoverySnapshot(t, tx, second.ID))
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
			require.Equal(t, beforeFirst, inboxRecoverySnapshot(t, pool, first.ID))
			require.Equal(t, beforeSecond, inboxRecoverySnapshot(t, pool, second.ID))
		})
	}
}

func TestApplyInboxRecoveryValidatesBeforeMutation(t *testing.T) {
	pool := newMigratedPool(t)
	first := seedInboxRecovery(t, pool, "valid-first", false)
	second := seedInboxRecovery(t, pool, "invalid-second", false)
	before := inboxRecoverySnapshot(t, pool, first.ID)
	tests := []struct {
		name   string
		change func(*pgstore.InboxRecoveryDecision)
	}{
		{name: "zero ID", change: func(d *pgstore.InboxRecoveryDecision) { d.ID = 0 }},
		{name: "negative ID", change: func(d *pgstore.InboxRecoveryDecision) { d.ID = -1 }},
		{name: "message", change: func(d *pgstore.InboxRecoveryDecision) { d.MessageID = "" }},
		{name: "attempts", change: func(d *pgstore.InboxRecoveryDecision) { d.Attempts = -1 }},
		{name: "lease", change: func(d *pgstore.InboxRecoveryDecision) { d.LeaseUntil = time.Time{} }},
		{name: "target", change: func(d *pgstore.InboxRecoveryDecision) { d.Status = "processing" }},
		{name: "pending reason", change: func(d *pgstore.InboxRecoveryDecision) { d.Status = "pending" }},
		{name: "review reason", change: func(d *pgstore.InboxRecoveryDecision) { d.Status = "manual_review"; d.Reason = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalid := second
			tt.change(&invalid)

			store, tx := inboxRecoveryTransaction(t, pool, pgstore.Options{Scope: inboxRecoveryScope})
			err := store.ApplyInboxRecovery(t.Context(), []pgstore.InboxRecoveryDecision{first, invalid})
			require.Error(t, err)
			require.NotErrorIs(t, err, pgstore.ErrClaimLost)
			require.Equal(t, before, inboxRecoverySnapshot(t, tx, first.ID), "validation must precede the entire batch")
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
		})
	}

	store := newScopedStoreWithOptions(t, pool, pgstore.Options{Scope: inboxRecoveryScope})
	require.Error(t, store.ApplyInboxRecovery(t.Context(), []pgstore.InboxRecoveryDecision{first}))
	require.Error(t, store.ApplyInboxRecovery(t.Context(), nil))
	require.Equal(t, before, inboxRecoverySnapshot(t, pool, first.ID), "pool must not open its own transaction")

	bound, tx := inboxRecoveryTransaction(t, pool, store.Options())
	require.NoError(t, bound.ApplyInboxRecovery(t.Context(), nil))
	require.NoError(t, pgxdb.Rollback(t.Context(), tx))
}

func TestApplyInboxRecoveryTerminalPayloadPolicy(t *testing.T) {
	pool := newMigratedPool(t)

	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprintf("discard=%t", discard), func(t *testing.T) {
			decisions := make([]pgstore.InboxRecoveryDecision, 0, 8)

			for _, pending := range []bool{false, true} {
				for _, target := range []struct {
					status string
					reason string
				}{
					{status: "pending"},
					{status: inboxCompletedStatus},
					{status: inboxCompletedStatus, reason: "reconciled"},
					{status: "manual_review", reason: "delivery ambiguous"},
				} {
					messageID := fmt.Sprintf("%s/%t/%s/%s", t.Name(), pending, target.status, target.reason)
					decision := seedInboxRecovery(t, pool, messageID, pending)

					decision.Status = target.status
					decision.Reason = target.reason
					decisions = append(decisions, decision)
				}
			}

			store, tx := inboxRecoveryTransaction(t, pool, pgstore.Options{
				Scope: inboxRecoveryScope, DiscardInboxManualReviewPayload: discard,
			})

			var appliedAt time.Time

			require.NoError(t, tx.QueryRow(t.Context(), "SELECT now()").Scan(&appliedAt))
			require.NoError(t, store.ApplyInboxRecovery(t.Context(), decisions))
			require.NoError(t, tx.Commit(t.Context()))

			for _, decision := range decisions {
				assertInboxRecoveryResult(t, pool, decision, appliedAt, discard)
			}

			// A terminal row cannot be changed again using its former claim snapshot.
			again, retryTx := inboxRecoveryTransaction(t, pool, store.Options())
			require.ErrorIs(t, again.ApplyInboxRecovery(t.Context(), decisions[1:2]), pgstore.ErrClaimLost)
			require.NoError(t, pgxdb.Rollback(t.Context(), retryTx))
		})
	}
}

func seedInboxRecovery(t *testing.T, db pgstore.Querier, messageID string, pending bool) pgstore.InboxRecoveryDecision {
	t.Helper()

	decision := pgstore.InboxRecoveryDecision{MessageID: messageID, Status: inboxCompletedStatus, Reason: "reconciled"}

	const query = `INSERT INTO iris_webhook_inbox
		(scope, message_id, ordering_key, payload, status, attempts, claim_token, lease_until, available_at, updated_at)
		VALUES ('recovery', $1, $1, '{"private":"body"}'::jsonb,
		        CASE WHEN $2 THEN 'pending' ELSE 'processing' END, 2,
		        CASE WHEN $2 THEN NULL ELSE 'original-token' END,
		        CASE WHEN $2 THEN NULL ELSE now() - interval '1 hour' END,
		        now() + interval '1 day', now() - interval '1 hour')
		RETURNING id, attempts, coalesce(claim_token, ''), coalesce(lease_until, '0001-01-01 UTC'::timestamptz)`

	require.NoError(t, db.QueryRow(t.Context(), query, messageID, pending).
		Scan(&decision.ID, &decision.Attempts, &decision.ClaimToken, &decision.LeaseUntil))

	return decision
}

func inboxRecoveryTransaction(t *testing.T, pool *pgxpool.Pool, opts pgstore.Options) (*pgstore.Store, pgx.Tx) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), tx); rollbackErr != nil {
			t.Errorf("rollback recovery: %v", rollbackErr)
		}
	})

	store, err := pgstore.New(tx, opts)
	require.NoError(t, err)

	return store, tx
}

func inboxRecoverySnapshot(t *testing.T, db pgstore.Querier, id int64) string {
	t.Helper()

	var snapshot string

	require.NoError(t, db.QueryRow(t.Context(),
		"SELECT to_jsonb(inbox)::text FROM iris_webhook_inbox AS inbox WHERE id = $1", id).Scan(&snapshot))

	return snapshot
}

func assertInboxRecoveryResult(t *testing.T, db pgstore.Querier, decision pgstore.InboxRecoveryDecision, appliedAt time.Time, discard bool) {
	t.Helper()

	var (
		status, payload, messageID string
		reason, token              *string
		lease, terminal            *time.Time
		available, updated         time.Time
		attempts                   int
	)

	const query = `SELECT status, payload::text, message_id, terminal_reason, claim_token,
		lease_until, terminal_at, available_at, updated_at, attempts FROM iris_webhook_inbox WHERE id = $1`

	require.NoError(t, db.QueryRow(t.Context(), query, decision.ID).
		Scan(&status, &payload, &messageID, &reason, &token, &lease, &terminal, &available, &updated, &attempts))
	require.Equal(t, decision.Status, status)
	require.Equal(t, decision.MessageID, messageID)
	require.Equal(t, decision.Attempts, attempts)
	require.Nil(t, token)
	require.Nil(t, lease)
	require.True(t, available.Equal(appliedAt))
	require.True(t, updated.Equal(appliedAt))

	if decision.Status == "pending" {
		require.Nil(t, terminal)
	} else {
		require.NotNil(t, terminal)
		require.True(t, terminal.Equal(appliedAt))
	}

	if decision.Reason == "" {
		require.Nil(t, reason)
	} else {
		require.NotNil(t, reason)
		require.Equal(t, decision.Reason, *reason)
	}

	expectedPayload := `{"private":"body"}`

	if decision.Status == inboxCompletedStatus || (decision.Status == "manual_review" && discard) {
		expectedPayload = `{}`
	}

	require.JSONEq(t, expectedPayload, payload)
}
