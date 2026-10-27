package pgstore_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

const backlogRows = 5000

// TestClaimBatchSkipsBlockedBacklogKey는 한 key에 5,000행 backlog가 있고 그 head가 재시도 대기 중일 때
// 다른 key의 head가 한 번의 ClaimBatch로 반환되는지 본다. 후속 행이 후보에 남으면 claim이 backlog를
// 매번 순회하므로, 후보는 key별 head뿐이어야 한다. 같은 key의 FIFO와 잠긴 head의 건너뛰기도 함께 본다.
func TestClaimBatchSkipsBlockedBacklogKey(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()
	scope := store.Options().Scope

	seedOrderedInbox(t, pool, scope, "backlog-", "key-backlog", backlogRows, "1 hour")
	seedOrderedInbox(t, pool, scope, "other-", "key-other", 3, "1 minute")

	first, err := store.ClaimBatch(ctx, 10)
	if err != nil || len(first) != 2 {
		t.Fatalf("first ClaimBatch = %d claims, %v; want one head per key", len(first), err)
	}

	deferBacklogAndCompleteOther(t, store, first)

	next, err := store.ClaimBatch(ctx, 10)
	if err != nil || len(next) != 1 || next[0].MessageID != "other-2" {
		t.Fatalf("ClaimBatch behind a deferred backlog head = %+v, %v; want only other-2", next, err)
	}

	if completeErr := store.Complete(ctx, next[0], ""); completeErr != nil {
		t.Fatalf("complete other-2: %v", completeErr)
	}

	// 다른 트랜잭션이 잠근 head는 건너뛰고, 같은 key의 후속 행으로 넘어가지 않는다.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin locking transaction: %v", err)
	}

	defer func() {
		if rollbackErr := pgxdb.Rollback(ctx, tx); rollbackErr != nil {
			t.Errorf("rollback locking transaction: %v", rollbackErr)
		}
	}()

	if _, lockErr := tx.Exec(ctx, `SELECT id FROM iris_webhook_inbox WHERE scope = $1 AND message_id = 'other-3' FOR UPDATE`, scope); lockErr != nil {
		t.Fatalf("lock other-3: %v", lockErr)
	}

	locked, err := store.ClaimBatch(ctx, 10)
	if err != nil || len(locked) != 0 {
		t.Fatalf("ClaimBatch with the only ready head locked = %+v, %v; want none", locked, err)
	}
}

// seedOrderedInbox는 한 ordering key에 created_at이 1ms씩 늘어나는 pending 행 count개를 넣는다.
func seedOrderedInbox(t *testing.T, pool *pgxpool.Pool, scope, prefix, key string, count int, age string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, created_at)
		SELECT $1, $2 || v, $3, '{}'::jsonb, now() - $5::interval + v * interval '1 millisecond'
		FROM generate_series(1, $4) AS v`, scope, prefix, key, count, age)
	if err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

// deferBacklogAndCompleteOther는 backlog head를 한 시간 뒤로 미루고 다른 key의 head를 끝낸다.
func deferBacklogAndCompleteOther(t *testing.T, store *pgstore.Store, claims []pgstore.InboxClaim) {
	t.Helper()

	for index := range claims {
		claim := &claims[index]

		var err error

		switch claim.MessageID {
		case "backlog-1":
			err = store.Release(t.Context(), *claim, time.Hour)
		case "other-1":
			err = store.Complete(t.Context(), *claim, "")
		default:
			t.Fatalf("first ClaimBatch returned %q; want only the oldest row of each key", claim.MessageID)
		}

		if err != nil {
			t.Fatalf("settle %s: %v", claim.MessageID, err)
		}
	}
}

// TestReleaseClaimTokenReturnsUnreturnedClaims는 호출자가 처리하지 못한 claim을 token 단위로 즉시 되돌려
// 같은 ordering key가 lease 만료까지 막히지 않고, attempts가 원복되며, 이미 종단인 행은 바꾸지 않는지 본다.
func TestReleaseClaimTokenReturnsUnreturnedClaims(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope: "release-token-scope", Lease: time.Hour,
	})
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, created_at)
		SELECT 'release-token-scope', 'message-' || v, 'key-' || (v % 3), '{}'::jsonb, now() - interval '1 minute' + v * interval '1 millisecond'
		FROM generate_series(1, 6) AS v`)
	if err != nil {
		t.Fatalf("seed inbox: %v", err)
	}

	claims, err := store.ClaimBatch(ctx, 3)
	if err != nil || len(claims) != 3 {
		t.Fatalf("ClaimBatch = %d claims, %v; want 3 heads", len(claims), err)
	}

	if completeErr := store.Complete(ctx, claims[0], ""); completeErr != nil {
		t.Fatalf("complete one claim: %v", completeErr)
	}

	released, err := store.ReleaseClaimToken(ctx, claims[0].ClaimToken)
	if err != nil || released != 2 {
		t.Fatalf("ReleaseClaimToken = %d, %v; want the two claims that were not completed", released, err)
	}

	var pendingUnattempted int

	if scanErr := pool.QueryRow(ctx, `
		SELECT count(*) FROM iris_webhook_inbox
		WHERE scope = 'release-token-scope' AND id = ANY($1) AND status = 'pending' AND attempts = 0 AND claim_token IS NULL`,
		[]int64{claims[1].ID, claims[2].ID}).Scan(&pendingUnattempted); scanErr != nil {
		t.Fatalf("inspect released rows: %v", scanErr)
	}

	if pendingUnattempted != 2 {
		t.Fatalf("released pending rows with restored attempts = %d; want 2", pendingUnattempted)
	}

	again, err := store.ClaimBatch(ctx, 3)
	if err != nil || len(again) != 3 {
		t.Fatalf("ClaimBatch after release = %d claims, %v; want the released heads and the completed key's next row", len(again), err)
	}

	if unknown, unknownErr := store.ReleaseClaimToken(ctx, "unknown-token"); unknownErr != nil || unknown != 0 {
		t.Fatalf("ReleaseClaimToken(unknown) = %d, %v; want 0", unknown, unknownErr)
	}
}

// TestPruneInboxLargeBacklogHonorsLimitPerBranch는 기한이 지난 completed 5,000행과 manual_review 행이
// 섞여 있어도 한 호출이 정확히 limit개만 지우고, manual_review 보존이 0이면 그 갈래를 지우지 않으며,
// 보존이 있으면 completed를 비운 뒤 남은 한도로만 manual_review를 지우는지 본다.
func TestPruneInboxLargeBacklogHonorsLimitPerBranch(t *testing.T) {
	pool := newMigratedPool(t)
	ctx := t.Context()

	const scope = "prune-backlog"

	_, err := pool.Exec(ctx, `
		INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, terminal_at, terminal_reason)
		SELECT $1, 'old-' || v, 'key', '{}'::jsonb,
		       CASE WHEN v <= $2 THEN 'completed' ELSE 'manual_review' END,
		       now() - interval '400 days' + v * interval '1 second',
		       CASE WHEN v <= $2 THEN NULL ELSE 'review' END
		FROM generate_series(1, $2 + 100) AS v`, scope, backlogRows)
	if err != nil {
		t.Fatalf("seed terminal backlog: %v", err)
	}

	keepReviews := newScopedStoreWithOptions(t, pool, pgstore.Options{Scope: scope})

	for call := range backlogRows / 1000 {
		pruned, err := keepReviews.PruneInbox(ctx, 1000)
		if err != nil || pruned != 1000 {
			t.Fatalf("prune call %d = %d, %v; want exactly the limit", call, pruned, err)
		}
	}

	if pruned, err := keepReviews.PruneInbox(ctx, 1000); err != nil || pruned != 0 {
		t.Fatalf("prune with no manual review retention = %d, %v; want manual review rows kept", pruned, err)
	}

	expireReviews := newScopedStoreWithOptions(t, pool, pgstore.Options{Scope: scope, InboxManualReviewRetention: testManualReviewRetention})

	if pruned, err := expireReviews.PruneInbox(ctx, 60); err != nil || pruned != 60 {
		t.Fatalf("prune manual review branch = %d, %v; want the limit", pruned, err)
	}

	if pruned, err := expireReviews.PruneInbox(ctx, 60); err != nil || pruned != 40 {
		t.Fatalf("prune remaining manual review rows = %d, %v; want 40", pruned, err)
	}
}
