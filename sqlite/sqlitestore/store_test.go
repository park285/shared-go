package sqlitestore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"
	"github.com/park285/shared-go/sqlite/sqlitestore"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const testScope = "contract"

func testOptions() sqlitestore.Options {
	return sqlitestore.Options{
		Scope:                           testScope,
		IrisAdmissionRetention:          time.Hour,
		Lease:                           time.Minute,
		InboxTerminalRetention:          2 * time.Hour,
		InboxManualReviewRetention:      2 * time.Hour,
		DiscardInboxManualReviewPayload: true,
	}
}

func openStore(t *testing.T) (*sqlitestore.Store, *sqlitedb.DB) {
	t.Helper()

	db, err := sqlitedb.Open(t.Context(), sqlitedb.Options{
		Path:               filepath.Join(t.TempDir(), "state", "store.db"),
		ReadConns:          2,
		ConnMaxIdleTime:    time.Minute,
		BusyTimeout:        5 * time.Second,
		JournalSizeLimit:   8 << 20,
		CheckpointInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := db.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	})

	schema, err := os.ReadFile("testdata/schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	if migrateErr := db.Migrate(t.Context(), []sqlitedb.Migration{{Name: "0001_schema.sql", SQL: string(schema)}}); migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}

	store, err := sqlitestore.New(db, testOptions())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return store, db
}

func TestIrisDurableContract(t *testing.T) {
	contracttest.Run(t, contracttest.Suite{
		Admitter: func(t *testing.T) irisdurable.Admitter {
			t.Helper()

			store, _ := openStore(t)

			return store
		},
		NonceStore: func(t *testing.T) irisdurable.NonceStore {
			t.Helper()

			store, _ := openStore(t)

			return store
		},
		NonceExpiry: 200 * time.Millisecond,
	})
}

func admit(t *testing.T, store *sqlitestore.Store, messageID, orderingKey string) {
	t.Helper()

	result, err := store.Admit(t.Context(), irisdurable.AdmissionInput{
		MessageID: messageID, OrderingKey: orderingKey, Payload: []byte(`{"messageId":"` + messageID + `"}`),
	})
	if err != nil || result != workercontract.AdmissionAccepted {
		t.Fatalf("admit %s = %s, %v", messageID, result, err)
	}
}

func claimIDs(claims []sqlitestore.InboxClaim) []string {
	ids := make([]string, 0, len(claims))
	for i := range claims {
		ids = append(ids, claims[i].MessageID)
	}

	return ids
}

func requireMessages(t *testing.T, claims []sqlitestore.InboxClaim, want ...string) {
	t.Helper()

	got := claimIDs(claims)
	if len(got) != len(want) {
		t.Fatalf("claimed %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claimed %v, want %v", got, want)
		}
	}
}

func TestClaimBatchTakesOneHeadPerOrderingKey(t *testing.T) {
	store, _ := openStore(t)

	admit(t, store, "a1", "room-a")
	admit(t, store, "a2", "room-a")
	admit(t, store, "b1", "room-b")

	claims, err := store.ClaimBatch(t.Context(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	requireMessages(t, claims, "a1", "b1")

	if claims[0].ClaimToken == "" || claims[0].ClaimToken != claims[1].ClaimToken || claims[0].Attempts != 1 {
		t.Fatalf("claims share one token and count the attempt: %+v", claims)
	}

	next, err := store.ClaimBatch(t.Context(), 10)
	if err != nil || len(next) != 0 {
		t.Fatalf("a claimed head keeps its successor waiting: %v, %v", claimIDs(next), err)
	}

	if completeErr := store.Complete(t.Context(), claims[0], ""); completeErr != nil {
		t.Fatalf("complete: %v", completeErr)
	}

	next, err = store.ClaimBatch(t.Context(), 10)
	if err != nil {
		t.Fatalf("claim successor: %v", err)
	}

	requireMessages(t, next, "a2")
}

func TestExpiredLeaseKeepsBlockingItsOrderingKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, _ := openStore(t)

		admit(t, store, "m1", "room")
		admit(t, store, "m2", "room")

		claims, err := store.ClaimBatch(t.Context(), 10)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}

		requireMessages(t, claims, "m1")

		time.Sleep(2 * testOptions().Lease)

		again, err := store.ClaimBatch(t.Context(), 10)
		if err != nil || len(again) != 0 {
			t.Fatalf("claim after lease expiry = %v, %v; want nothing until explicit recovery", claimIDs(again), err)
		}

		snapshot, err := store.InboxReadySnapshot(t.Context())
		if err != nil || snapshot.Ready != 0 {
			t.Fatalf("ready snapshot after lease expiry = %+v, %v; want 0", snapshot, err)
		}
	})
}

func TestFencedTransitionsRejectAnotherToken(t *testing.T) {
	store, _ := openStore(t)

	admit(t, store, "m1", "room")

	claims, err := store.ClaimBatch(t.Context(), 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v, %v", claims, err)
	}

	stale := claims[0]

	stale.ClaimToken = "stale"

	for name, transition := range map[string]func() error{
		"complete":      func() error { return store.Complete(t.Context(), stale, "") },
		"release":       func() error { return store.ReleaseInboxAt(t.Context(), stale, time.Now()) },
		"defer":         func() error { return store.DeferInboxUntil(t.Context(), stale, time.Now()) },
		"manual review": func() error { return store.ManualReviewInbox(t.Context(), stale, "operator") },
	} {
		if transitionErr := transition(); !errors.Is(transitionErr, sqlitestore.ErrClaimLost) {
			t.Fatalf("%s with another token = %v, want ErrClaimLost", name, transitionErr)
		}
	}

	if deferErr := store.DeferInboxUntil(t.Context(), claims[0], time.Now().Add(time.Hour)); deferErr != nil {
		t.Fatalf("defer: %v", deferErr)
	}

	snapshot, err := store.InboxReadySnapshot(t.Context())
	if err != nil || snapshot.Ready != 0 {
		t.Fatalf("a deferred head is not ready: %+v, %v", snapshot, err)
	}

	again, err := store.ClaimBatch(t.Context(), 1)
	if err != nil || len(again) != 0 {
		t.Fatalf("a deferred head is not claimable: %v, %v", claimIDs(again), err)
	}
}

func TestReleaseClaimTokenKeepsReturnedRows(t *testing.T) {
	store, _ := openStore(t)

	admit(t, store, "a1", "room-a")
	admit(t, store, "b1", "room-b")

	claims, err := store.ClaimBatch(t.Context(), 10)
	if err != nil || len(claims) != 2 {
		t.Fatalf("claim: %v, %v", claimIDs(claims), err)
	}

	released, err := store.ReleaseClaimToken(t.Context(), claims[0].ClaimToken, []int64{claims[0].ID})
	if err != nil || released != 1 {
		t.Fatalf("release = %d, %v; want 1", released, err)
	}

	reclaimed, err := store.ClaimBatch(t.Context(), 10)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	requireMessages(t, reclaimed, "b1")

	if reclaimed[0].Attempts != 1 {
		t.Fatalf("a released claim does not count an attempt: %+v", reclaimed[0])
	}

	released, err = store.ReleaseClaimToken(t.Context(), reclaimed[0].ClaimToken, nil)
	if err != nil || released != 1 {
		t.Fatalf("release without kept rows = %d, %v; want 1", released, err)
	}
}

func TestApplyInboxRecoveryNeedsTheObservedLease(t *testing.T) {
	store, db := openStore(t)

	admit(t, store, "m1", "room")

	claims, err := store.ClaimBatch(t.Context(), 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v, %v", claims, err)
	}

	if outsideErr := store.ApplyInboxRecovery(t.Context(), nil); outsideErr == nil {
		t.Fatal("recovery outside a transaction must fail")
	}

	var leaseMS int64

	if readErr := db.Reader().QueryRowContext(t.Context(), "SELECT lease_until FROM iris_webhook_inbox WHERE message_id = 'm1'").Scan(&leaseMS); readErr != nil {
		t.Fatalf("read lease: %v", readErr)
	}

	decision := sqlitestore.InboxRecoveryDecision{
		ID: claims[0].ID, MessageID: "m1", ClaimToken: claims[0].ClaimToken, Attempts: 1,
		LeaseUntil: time.UnixMilli(leaseMS + 1), Status: "manual_review", Reason: "lease expired",
	}

	err = db.WriteTx(t.Context(), func(tx *sql.Tx) error {
		return store.WithTx(tx).ApplyInboxRecovery(t.Context(), []sqlitestore.InboxRecoveryDecision{decision})
	})
	if !errors.Is(err, sqlitestore.ErrClaimLost) {
		t.Fatalf("recovery with a different lease = %v, want ErrClaimLost", err)
	}

	decision.LeaseUntil = time.UnixMilli(leaseMS)

	if err := db.WriteTx(t.Context(), func(tx *sql.Tx) error {
		return store.WithTx(tx).ApplyInboxRecovery(t.Context(), []sqlitestore.InboxRecoveryDecision{decision})
	}); err != nil {
		t.Fatalf("recovery: %v", err)
	}

	var status, payload, reason string

	if err := db.Reader().QueryRowContext(t.Context(),
		"SELECT status, payload, terminal_reason FROM iris_webhook_inbox WHERE message_id = 'm1'").Scan(&status, &payload, &reason); err != nil {
		t.Fatalf("read row: %v", err)
	}

	if status != "manual_review" || payload != "{}" || reason != "lease expired" {
		t.Fatalf("row = %s %s %s; want manual_review with a discarded payload", status, payload, reason)
	}
}

func TestPruneInboxBeforeKeepsRowsInsideRetention(t *testing.T) {
	store, db := openStore(t)

	for _, id := range []string{"old", "fresh"} {
		admit(t, store, id, "room-"+id)
	}

	claims, err := store.ClaimBatch(t.Context(), 10)
	if err != nil || len(claims) != 2 {
		t.Fatalf("claim: %v, %v", claimIDs(claims), err)
	}

	for i := range claims {
		if completeErr := store.Complete(t.Context(), claims[i], ""); completeErr != nil {
			t.Fatalf("complete: %v", completeErr)
		}
	}

	if ageErr := db.WriteTx(t.Context(), func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(t.Context(), "UPDATE iris_webhook_inbox SET terminal_at = ?1 WHERE message_id = 'old'",
			time.Now().Add(-3*time.Hour).UnixMilli()); execErr != nil {
			return fmt.Errorf("age terminal row: %w", execErr)
		}

		return nil
	}); ageErr != nil {
		t.Fatalf("age row: %v", ageErr)
	}

	pruned, err := store.PruneInboxBefore(t.Context(), time.Now(), 10)
	if err != nil || pruned != 1 {
		t.Fatalf("prune = %d, %v; want 1", pruned, err)
	}

	result, err := store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "fresh", OrderingKey: "room-fresh", Payload: []byte("{}")})
	if err != nil || result != workercontract.AdmissionDuplicate {
		t.Fatalf("a retained terminal row still deduplicates: %s, %v", result, err)
	}
}

func TestAdmitRejectsAPayloadThatIsNotAnObject(t *testing.T) {
	store, _ := openStore(t)

	result, err := store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "m1", OrderingKey: "room", Payload: []byte("[]")})
	if err == nil || result != workercontract.AdmissionRejected {
		t.Fatalf("admit array payload = %s, %v; want rejected", result, err)
	}
}

func TestAdmitReportsAnUnknownOutcomeWhenCommitFails(t *testing.T) {
	store, db := openStore(t)

	if _, err := db.Writer().ExecContext(t.Context(), `
CREATE TABLE commit_probe_parent (id INTEGER PRIMARY KEY);
CREATE TABLE commit_probe (parent_id INTEGER REFERENCES commit_probe_parent (id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER commit_probe_on_admit AFTER INSERT ON iris_webhook_inbox WHEN NEW.message_id = 'commit-fails'
BEGIN
    INSERT INTO commit_probe (parent_id) VALUES (1);
END;`); err != nil {
		t.Fatalf("install commit probe: %v", err)
	}

	result, err := store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "commit-fails", OrderingKey: "room", Payload: []byte("{}")})
	if !errors.Is(err, sqlitedb.ErrCommit) || result != workercontract.AdmissionOutcomeUnknown {
		t.Fatalf("admit with a failing commit = %s, %v; want outcome unknown", result, err)
	}

	var rows int

	if err := db.Reader().QueryRowContext(t.Context(), "SELECT count(*) FROM iris_webhook_inbox").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("a failed commit leaves %d rows, %v", rows, err)
	}
}

func TestPruneNonceReportsNoRowsWhenTheCommitFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, db := openStore(t)

		if _, err := db.Writer().ExecContext(t.Context(), `
CREATE TABLE commit_probe_parent (id INTEGER PRIMARY KEY);
CREATE TABLE commit_probe (parent_id INTEGER REFERENCES commit_probe_parent (id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER commit_probe_on_prune AFTER DELETE ON iris_nonce
BEGIN
    INSERT INTO commit_probe (parent_id) VALUES (1);
END;`); err != nil {
			t.Fatalf("install commit probe: %v", err)
		}

		if duplicate, err := store.IsDuplicate(t.Context(), "expiring", time.Millisecond); err != nil || duplicate {
			t.Fatalf("first observation = %v, %v", duplicate, err)
		}

		time.Sleep(5 * time.Millisecond)

		pruned, err := store.PruneNonce(t.Context(), 10)
		if !errors.Is(err, sqlitedb.ErrCommit) || pruned != 0 {
			t.Fatalf("prune nonce with a failing commit = %d, %v; want 0 and an unknown commit", pruned, err)
		}
	})
}

func TestPruneNonceRemovesExpiredKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store, _ := openStore(t)

		if duplicate, err := store.IsDuplicate(t.Context(), "expiring", time.Millisecond); err != nil || duplicate {
			t.Fatalf("first observation = %v, %v", duplicate, err)
		}

		if duplicate, err := store.IsDuplicate(t.Context(), "kept", time.Hour); err != nil || duplicate {
			t.Fatalf("first observation = %v, %v", duplicate, err)
		}

		time.Sleep(5 * time.Millisecond)

		pruned, err := store.PruneNonce(t.Context(), 10)
		if err != nil || pruned != 1 {
			t.Fatalf("prune nonce = %d, %v; want 1", pruned, err)
		}

		if duplicate, err := store.IsDuplicate(t.Context(), "kept", time.Hour); err != nil || !duplicate {
			t.Fatalf("an unexpired key stays a duplicate: %v, %v", duplicate, err)
		}
	})
}

func TestInboxHeadReadsLookUpEachHeadAfterIdleStatistics(t *testing.T) {
	_, db := openStore(t)

	err := db.WriteTx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, attempts, available_at, terminal_at, created_at, updated_at)
SELECT ?1, 'done-' || value, 'room-' || (value % 100), '{}', 'completed', 1, value, value, value, value FROM generate_series(1, 2000)`, testScope); err != nil {
			return fmt.Errorf("insert terminal rows: %w", err)
		}

		if _, err := tx.ExecContext(t.Context(), "ANALYZE"); err != nil {
			return fmt.Errorf("analyze: %w", err)
		}

		if _, err := tx.ExecContext(t.Context(), `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, available_at, created_at, updated_at)
SELECT ?1, 'wait-' || value, 'room-' || (value % 100), '{}', 'pending', 0, 3000 + value, 3000 + value FROM generate_series(1, 500)`, testScope); err != nil {
			return fmt.Errorf("insert pending rows: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	queries := os.DirFS("queries")

	for name, args := range map[string][]any{
		"claim_candidates.sql":     {testScope, int64(1) << 40, 32},
		"inbox_ready_snapshot.sql": {testScope, int64(1) << 40},
	} {
		query, readErr := fs.ReadFile(queries, name)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}

		for _, detail := range queryPlan(t, db, string(query), args...) {
			if strings.HasPrefix(detail, "SCAN current_row") {
				t.Errorf("%s scans the inbox for every head once idle statistics call the pending index empty: %s", name, detail)
			}
		}
	}
}

func queryPlan(t *testing.T, db *sqlitedb.DB, query string, args ...any) []string {
	t.Helper()

	rows, err := db.Reader().QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	defer rows.Close()

	var details []string

	for rows.Next() {
		var id, parent, unused int

		var detail string

		if scanErr := rows.Scan(&id, &parent, &unused, &detail); scanErr != nil {
			t.Fatalf("scan plan: %v", scanErr)
		}

		details = append(details, detail)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("read plan: %v", rowsErr)
	}

	return details
}
