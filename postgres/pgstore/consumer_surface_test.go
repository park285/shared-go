package pgstore_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgstore"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

func TestSettleCountsOnlyStalledAttemptsAgainstTheCap(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	record := fixture.NewRecord(t, []byte(`{"type":"text","text":"chunked"}`))
	if _, err := store.Stage(ctx, record); err != nil {
		t.Fatalf("stage: %v", err)
	}

	progressed := settleOnce(t, store, record.ReplyIdentity, true)
	if progressed != 0 {
		t.Fatalf("attempts after a progressing attempt = %d; want 0, the cap counts only stalled attempts", progressed)
	}

	stalled := settleOnce(t, store, record.ReplyIdentity, false)
	if stalled != 1 {
		t.Fatalf("attempts after a stalled attempt = %d; want 1", stalled)
	}
}

func settleOnce(t *testing.T, store *pgstore.Store, identity irisdurable.ReplyIdentity, progressed bool) int {
	t.Helper()

	attempt, err := store.BeginAttempt(t.Context(), identity)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	if settleErr := store.Settle(t.Context(), attempt, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusOutcomeUnknown,
		ClientRequestID: attempt.ClientRequestID,
		Progressed:      progressed,
	}); settleErr != nil {
		t.Fatalf("settle: %v", settleErr)
	}

	state, err := store.Inspect(t.Context(), identity)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	return state.Attempts
}

func TestStageRejectsALateLowerOrdinal(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	base := fixture.NewRecord(t, []byte(`{"type":"text","text":"second"}`))
	second := irisdurable.ReplyRecord{
		MessageID: base.MessageID, Phase: base.Phase, Ordinal: 1,
		RoomID:          base.RoomID,
		ClientRequestID: base.ClientRequestID + ".1",
		Payload:         base.Payload,
	}

	if outcome, err := store.Stage(ctx, second); err != nil || outcome != irisdurable.ReplyStaged {
		t.Fatalf("stage ordinal 1 = (%q, %v); want staged", outcome, err)
	}

	first := irisdurable.ReplyRecord{
		MessageID: base.MessageID, Phase: base.Phase, Ordinal: 0,
		RoomID:          base.RoomID,
		ClientRequestID: base.ClientRequestID + ".0",
		Payload:         []byte(`{"type":"text","text":"first"}`),
	}

	staged, err := store.StageRow(ctx, first)
	if err != nil {
		t.Fatalf("stage the late lower ordinal: %v", err)
	}

	if staged.Outcome != irisdurable.ReplyOrdinalSuperseded {
		t.Fatalf("stage outcome = %q; want %q", staged.Outcome, irisdurable.ReplyOrdinalSuperseded)
	}

	if _, inspectErr := store.Inspect(ctx, first.ReplyIdentity); !errors.Is(inspectErr, pgstore.ErrNotFound) {
		t.Fatalf("inspect the rejected ordinal = %v; want ErrNotFound, no row may be written", inspectErr)
	}

	if outcome, restageErr := store.Stage(ctx, second); restageErr != nil || outcome != irisdurable.ReplyAlreadyStaged {
		t.Fatalf("restage ordinal 1 = (%q, %v); want already_staged", outcome, restageErr)
	}
}

func TestStageRowReturnsTheStoredRow(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	record := fixture.NewRecord(t, []byte(`{"type":"text","text":"stored"}`))

	inserted, err := store.StageRow(ctx, record)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	switch {
	case inserted.Outcome != irisdurable.ReplyStaged:
		t.Fatalf("first stage outcome = %q; want staged", inserted.Outcome)
	case inserted.ID == 0:
		t.Fatal("first stage returned no row id")
	case inserted.Status != irisdurable.ReplyStatusPending:
		t.Fatalf("first stage status = %q; want pending", inserted.Status)
	}

	requireSameJSON(t, inserted.Payload, record.Payload, "first stage payload")

	if _, beginErr := store.BeginAttempt(ctx, record.ReplyIdentity); beginErr != nil {
		t.Fatalf("begin attempt: %v", beginErr)
	}

	again, err := store.StageRow(ctx, record)
	if err != nil {
		t.Fatalf("restage: %v", err)
	}

	switch {
	case again.Outcome != irisdurable.ReplyAlreadyStaged:
		t.Fatalf("restage outcome = %q; want already_staged", again.Outcome)
	case again.ID != inserted.ID:
		t.Fatalf("restage id = %d; want the stored row %d", again.ID, inserted.ID)
	case again.Status != irisdurable.ReplyStatusSubmitting:
		t.Fatalf("restage status = %q; want the row's current status submitting", again.Status)
	case again.Attempts != 1:
		t.Fatalf("restage attempts = %d; want the row's current attempts 1", again.Attempts)
	}

	diverged := irisdurable.ReplyRecord{
		ReplyIdentity:   record.ReplyIdentity,
		RoomID:          record.RoomID,
		ClientRequestID: record.ClientRequestID,
		Payload:         []byte(`{"type":"text","text":"different"}`),
	}

	divergedRow, err := store.StageRow(ctx, diverged)
	if err != nil {
		t.Fatalf("stage a diverging payload: %v", err)
	}

	if divergedRow.Outcome != irisdurable.ReplyPayloadDiverged {
		t.Fatalf("diverging stage outcome = %q; want payload_diverged", divergedRow.Outcome)
	}

	requireSameJSON(t, divergedRow.Payload, record.Payload, "diverging stage keeps the stored payload")
}

func requireSameJSON(t *testing.T, got, want []byte, label string) {
	t.Helper()

	var gotValue, wantValue any

	if err := jsonv2.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("%s: decode %s: %v", label, got, err)
	}

	if err := jsonv2.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("%s: decode the expected %s: %v", label, want, err)
	}

	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("%s = %s; want %s", label, got, want)
	}
}

func TestCompleteRequiresTheMessageIdAndALiveLease(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	claim := admitAndClaim(t, store, "complete-fence")
	foreign := pgstore.InboxClaim{ID: claim.ID, MessageID: claim.MessageID + "-other", ClaimToken: claim.ClaimToken}

	if err := store.Complete(ctx, foreign, ""); !errors.Is(err, pgstore.ErrClaimLost) {
		t.Fatalf("complete with a foreign message id = %v; want ErrClaimLost", err)
	}

	expireInboxLease(t, pool, claim.ID)

	if err := store.Complete(ctx, claim, ""); !errors.Is(err, pgstore.ErrClaimLost) {
		t.Fatalf("complete on an expired lease = %v; want ErrClaimLost", err)
	}

	if err := store.RenewInbox(ctx, claim); err != nil {
		t.Fatalf("renew the lapsed lease: %v", err)
	}

	if err := store.Complete(ctx, claim, ""); err != nil {
		t.Fatalf("complete after renewing: %v", err)
	}
}

func TestCompleteRecordsANonSuccessReason(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	claim := admitAndClaim(t, store, "complete-reason")

	if err := store.Complete(ctx, claim, "invalid_payload"); err != nil {
		t.Fatalf("complete with a reason: %v", err)
	}

	var (
		status string
		reason *string
	)

	const query = `SELECT status, terminal_reason FROM iris_webhook_inbox WHERE id = $1`

	if err := pool.QueryRow(ctx, query, claim.ID).Scan(&status, &reason); err != nil {
		t.Fatalf("read the completed row: %v", err)
	}

	if status != inboxCompletedStatus || reason == nil || *reason != "invalid_payload" {
		t.Fatalf("completed row = (%q, %v); want completed with the recorded reason", status, reason)
	}
}

func TestDeferReturnsTheAttemptBudget(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	claim := admitAndClaim(t, store, "defer-budget")
	if claim.Attempts != 1 {
		t.Fatalf("attempts after the first claim = %d; want 1", claim.Attempts)
	}

	if err := store.Defer(ctx, claim, 0); err != nil {
		t.Fatalf("defer: %v", err)
	}

	again, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim after a defer = (%v, %v); want the same message", ok, err)
	}

	if again.Attempts != 1 {
		t.Fatalf("attempts after a deferred claim = %d; want 1, a defer must not spend the budget", again.Attempts)
	}

	if releaseErr := store.Release(ctx, again, 0); releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	third, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim after a release = (%v, %v); want the same message", ok, err)
	}

	if third.Attempts != 2 {
		t.Fatalf("attempts after a released claim = %d; want 2, a release spends the budget", third.Attempts)
	}
}

func TestPruneInboxKeepsManualReviewLonger(t *testing.T) {
	pool := newMigratedPool(t)
	scope := fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), scopeCounter.Add(1))
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{Scope: scope})
	ctx := t.Context()

	completed := admitAndClaim(t, store, "prune-completed")
	if err := store.Complete(ctx, completed, ""); err != nil {
		t.Fatalf("complete: %v", err)
	}

	reviewed := admitAndClaim(t, store, "prune-review")
	if err := store.ManualReviewInbox(ctx, reviewed, "needs a human"); err != nil {
		t.Fatalf("manual review: %v", err)
	}

	backdateTerminalAt(t, pool, completed.ID, 400*24*time.Hour)
	backdateTerminalAt(t, pool, reviewed.ID, 400*24*time.Hour)

	pruned, err := store.PruneInbox(ctx, 10)
	if err != nil {
		t.Fatalf("prune with no manual review retention: %v", err)
	}

	if pruned != 1 {
		t.Fatalf("pruned %d rows; want only the completed row while manual review retention is unset", pruned)
	}

	withRetention := newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope:                      scope,
		InboxManualReviewRetention: testManualReviewRetention,
	})

	pruned, err = withRetention.PruneInbox(ctx, 10)
	if err != nil {
		t.Fatalf("prune with a manual review retention: %v", err)
	}

	if pruned != 1 {
		t.Fatalf("pruned %d rows; want the manual review row once its retention is set", pruned)
	}
}

func TestManualReviewRetentionMayNotUndercutTerminalRetention(t *testing.T) {
	t.Parallel()

	_, err := pgstore.New(&pgxpool.Pool{}, pgstore.Options{
		IrisAdmissionRetention: testIrisAdmissionRetention, InboxTerminalRetention: testInboxTerminalRetention,
		InboxManualReviewRetention: testInboxTerminalRetention - time.Hour,
	})
	if err == nil {
		t.Fatal("New with a manual review retention shorter than the terminal retention must fail")
	}
}

func TestSnapshotsMatchTheClaimPredicates(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	admitted := admitAndClaim(t, store, "snapshot-held")

	runtime, err := store.RuntimeSnapshot(ctx)
	if err != nil {
		t.Fatalf("inbox runtime snapshot: %v", err)
	}

	if runtime.Processing != 1 || runtime.Pending != 0 || runtime.Due != 0 {
		t.Fatalf("runtime snapshot = %+v; want one held row and nothing due", runtime)
	}

	ready, err := store.InboxReadySnapshot(ctx)
	if err != nil {
		t.Fatalf("inbox ready snapshot: %v", err)
	}

	if ready.Ready != 0 {
		t.Fatalf("inbox ready = %d; want 0 while the lease is live and Claim returns nothing", ready.Ready)
	}

	if releaseErr := store.Release(ctx, admitted, 0); releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	if ready, err = store.InboxReadySnapshot(ctx); err != nil || ready.Ready != 1 {
		t.Fatalf("inbox ready after a release = (%d, %v); want 1, matching what Claim would take", ready.Ready, err)
	}

	record := fixture.NewRecord(t, []byte(`{"type":"text","text":"snapshot"}`))
	if _, stageErr := store.Stage(ctx, record); stageErr != nil {
		t.Fatalf("stage: %v", stageErr)
	}

	counts, err := store.CountRepliesByStatus(ctx)
	if err != nil {
		t.Fatalf("count replies by status: %v", err)
	}

	if counts[irisdurable.ReplyStatusPending] != 1 {
		t.Fatalf("pending reply count = %d; want 1", counts[irisdurable.ReplyStatusPending])
	}

	replyReady, err := store.ReplyReadySnapshot(ctx)
	if err != nil {
		t.Fatalf("reply ready snapshot: %v", err)
	}

	candidates, err := store.Redrive(ctx, 10)
	if err != nil {
		t.Fatalf("redrive: %v", err)
	}

	if replyReady.Ready != int64(len(candidates)) {
		t.Fatalf("reply ready = %d but Redrive returned %d; the two predicates must agree", replyReady.Ready, len(candidates))
	}
}

func admitAndClaim(t *testing.T, store *pgstore.Store, label string) pgstore.InboxClaim {
	t.Helper()

	key := label + "-" + strconv.FormatUint(scopeCounter.Add(1), 10)

	if _, err := store.Admit(t.Context(), irisdurable.AdmissionInput{
		MessageID:   "msg-" + key,
		OrderingKey: "room-" + key,
		Payload:     []byte(`{"kind":"fence"}`),
	}); err != nil {
		t.Fatalf("admit %s: %v", key, err)
	}

	claim, ok, err := store.Claim(t.Context())
	if err != nil || !ok {
		t.Fatalf("claim %s = (%v, %v); want a claimed message", key, ok, err)
	}

	return claim
}

func expireInboxLease(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()

	const query = `UPDATE iris_webhook_inbox SET lease_until = clock_timestamp() - interval '1 second' WHERE id = $1`

	if _, err := pool.Exec(t.Context(), query, id); err != nil {
		t.Fatalf("expire the lease of %d: %v", id, err)
	}
}

func backdateTerminalAt(t *testing.T, pool *pgxpool.Pool, id int64, age time.Duration) {
	t.Helper()

	const query = `UPDATE iris_webhook_inbox SET terminal_at = now() - make_interval(secs => $2) WHERE id = $1`

	if _, err := pool.Exec(t.Context(), query, id, age.Seconds()); err != nil {
		t.Fatalf("backdate terminal_at of %d: %v", id, err)
	}
}

func TestStageSerializesTheOrdinalSequence(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	base := fixture.NewRecord(t, []byte(`{"type":"text","text":"sequence"}`))
	second := irisdurable.ReplyRecord{
		MessageID: base.MessageID, Phase: base.Phase, Ordinal: 1,
		RoomID:          base.RoomID,
		ClientRequestID: base.ClientRequestID + ".1",
		Payload:         base.Payload,
	}

	tx := beginStagedTx(t, pool, store.Options().Scope, second)

	first := irisdurable.ReplyRecord{
		MessageID: base.MessageID, Phase: base.Phase, Ordinal: 0,
		RoomID:          base.RoomID,
		ClientRequestID: base.ClientRequestID + ".0",
		Payload:         []byte(`{"type":"text","text":"late lower"}`),
	}

	late := stageAsync(store, first)

	select {
	case outcome := <-late:
		t.Fatalf("the late lower ordinal staged as %+v while ordinal 1 was uncommitted; the sequence was not serialized", outcome)
	case <-time.After(500 * time.Millisecond):
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit the staged successor: %v", commitErr)
	}

	select {
	case outcome := <-late:
		switch {
		case outcome.err != nil:
			t.Fatalf("the late lower ordinal after the commit: %v", outcome.err)
		case outcome.staged != irisdurable.ReplyOrdinalSuperseded:
			t.Fatalf("the late lower ordinal staged as %q; want %q", outcome.staged, irisdurable.ReplyOrdinalSuperseded)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the late lower ordinal never finished after the successor committed")
	}
}

type stageResult struct {
	staged irisdurable.ReplyStageOutcome
	err    error
}

func beginStagedTx(t *testing.T, pool *pgxpool.Pool, scope string, record irisdurable.ReplyRecord) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), tx); rollbackErr != nil {
			t.Errorf("rollback: %v", rollbackErr)
		}
	})

	store, err := pgstore.New(tx, testPolicy(pgstore.Options{Scope: scope}))
	if err != nil {
		t.Fatalf("new store on the transaction: %v", err)
	}

	if _, stageErr := store.Stage(t.Context(), record); stageErr != nil {
		t.Fatalf("stage inside the open transaction: %v", stageErr)
	}

	return tx
}

func stageAsync(store *pgstore.Store, record irisdurable.ReplyRecord) <-chan stageResult {
	done := make(chan stageResult, 1)

	go func() {
		staged, err := store.Stage(context.Background(), record)
		done <- stageResult{staged: staged, err: err}
	}()

	return done
}

func TestRetireReturnsTheRowsItClosed(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	record := fixture.NewRecord(t, []byte(`{"type":"image","fallbackText":"본문"}`))
	if _, err := store.Stage(ctx, record); err != nil {
		t.Fatalf("stage: %v", err)
	}

	attempt, err := store.BeginAttempt(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	if settleErr := store.Settle(ctx, attempt, irisdurable.ReplyOutcome{
		Status: irisdurable.ReplyStatusRetryablePreDispatch,
	}); settleErr != nil {
		t.Fatalf("settle: %v", settleErr)
	}

	exhausted := newScopedStoreWithOptions(t, pool, pgstore.Options{MaxAttempts: 1, Scope: store.Options().Scope})

	retired, err := retireAndCommit(t, pool, exhausted.Options())
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	if len(retired) != 1 {
		t.Fatalf("retired %d rows; want the exhausted row", len(retired))
	}

	closed := retired[0]
	if closed.ReplyIdentity != record.ReplyIdentity {
		t.Fatalf("retired identity = %+v; want %+v", closed.ReplyIdentity, record.ReplyIdentity)
	}

	if closed.Status != irisdurable.ReplyStatusRetryablePreDispatch {
		t.Fatalf("retired status = %q; want the status before retirement", closed.Status)
	}

	if closed.RoomID != record.RoomID || closed.ClientRequestID != record.ClientRequestID {
		t.Fatalf("retired row = (%q, %q); want the stored room and client request id", closed.RoomID, closed.ClientRequestID)
	}

	requireSameJSON(t, closed.Payload, record.Payload, "retired payload")

	state, err := store.Inspect(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if state.Status != irisdurable.ReplyStatusDead || state.PayloadPresent {
		t.Fatalf("stored row = (%q, payload present %t); want a scrubbed dead row", state.Status, state.PayloadPresent)
	}
}

func TestInboxPollDoesNotScanTheBacklog(t *testing.T) {
	const scope = "chatbotgo"

	pool := newMigratedPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, terminal_at, terminal_reason)
SELECT $1, 'sha256:' || lpad(to_hex(n), 64, '0'), 'sha256:' || repeat('a', 64), '{}',
       CASE WHEN n = 1 THEN 'manual_review' ELSE 'completed' END, now(), CASE WHEN n = 1 THEN 'callback_timeout_uncertain' END
FROM generate_series(1, 16384) n`, scope)
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload)
SELECT $1, 'sha256:' || repeat(k, 64), 'sha256:' || repeat(k, 64), '{}' FROM unnest(ARRAY['a','b']) k`, scope)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, "ANALYZE iris_webhook_inbox"); err != nil {
		t.Fatal(err)
	}

	checkInboxPlan(t, pool, "runtime snapshot", pgstore.InboxRuntimeSnapshotQueryForTest, scope)

	_, err = pool.Exec(ctx, `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, created_at)
SELECT $1, 'sha256:' || lpad(to_hex(16384+n), 64, '0'), 'sha256:' || repeat(CASE WHEN n % 2 = 0 THEN 'a' ELSE 'b' END, 64),
       '{}', now() - n * interval '1 second'
FROM generate_series(1, 16384) n`, scope)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, "ANALYZE iris_webhook_inbox"); err != nil {
		t.Fatal(err)
	}

	t.Run("ready", func(t *testing.T) {
		checkInboxPlan(t, pool, "ready snapshot", pgstore.InboxReadySnapshotQueryForTest, scope, false)
	})
	t.Run("claim", func(t *testing.T) {
		checkInboxPlan(t, pool, "claim", pgstore.ClaimInboxQueryForTest, scope, "plan-token", float64(60), 1, false)
	})
}

type inboxPlanNode struct {
	Rows     float64         `json:"Actual Rows"`
	Loops    float64         `json:"Actual Loops"`
	Filtered float64         `json:"Rows Removed by Filter"`
	Hits     int             `json:"Shared Hit Blocks"`
	Reads    int             `json:"Shared Read Blocks"`
	Plans    []inboxPlanNode `json:"Plans"`
}

func (p inboxPlanNode) maxRowsVisited() float64 {
	rows := (p.Rows + p.Filtered) * p.Loops
	for _, child := range p.Plans {
		rows = max(rows, child.maxRowsVisited())
	}

	return rows
}

func checkInboxPlan(t *testing.T, pool *pgxpool.Pool, label, query string, args ...any) {
	t.Helper()

	var raw []byte

	if err := pool.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN %s: %v", label, err)
	}

	var result []struct{ Plan inboxPlanNode }

	if err := jsonv2.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode %s plan: %v", label, err)
	}

	plan := result[0].Plan
	rows, blocks := plan.maxRowsVisited(), plan.Hits+plan.Reads
	t.Logf("%s: max rows visited by one node=%.2f, shared blocks=%d", label, rows, blocks)

	if rows <= 0 || rows > 32 || blocks <= 0 || blocks > 128 {
		t.Errorf("%s: max rows=%.2f (want 1..32), shared blocks=%d (want 1..128)", label, rows, blocks)
	}
}

func TestCountRepliesByStatusLeavesOutTerminalRows(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	pending := fixture.NewRecord(t, []byte(`{"type":"text","text":"pending"}`))
	if _, err := store.Stage(ctx, pending); err != nil {
		t.Fatalf("stage the pending reply: %v", err)
	}

	settled := fixture.NewRecord(t, []byte(`{"type":"text","text":"settled"}`))
	if _, err := store.Stage(ctx, settled); err != nil {
		t.Fatalf("stage the settled reply: %v", err)
	}

	attempt, err := store.BeginAttempt(ctx, settled.ReplyIdentity)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	if settleErr := store.Settle(ctx, attempt, irisdurable.ReplyOutcome{Status: irisdurable.ReplyStatusAccepted}); settleErr != nil {
		t.Fatalf("settle: %v", settleErr)
	}

	counts, err := store.CountRepliesByStatus(ctx)
	if err != nil {
		t.Fatalf("count replies by status: %v", err)
	}

	if counts[irisdurable.ReplyStatusPending] != 1 {
		t.Fatalf("pending count = %d; want the one row still waiting to go out", counts[irisdurable.ReplyStatusPending])
	}

	if _, present := counts[irisdurable.ReplyStatusAccepted]; present {
		t.Fatalf("accepted count = %d; want the terminal status absent", counts[irisdurable.ReplyStatusAccepted])
	}
}
