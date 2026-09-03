package pgstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

// TestSettleCountsOnlyStalledAttemptsAgainstTheCap은 전진한 시도가 재시도 예산을 쓰지 않는지
// 확인한다. 전진한 시도까지 세면 한 응답을 여러 조각으로 나눠 보내는 호출자에서 조각 수가
// (상한 / 조각당 왕복)을 넘는 순간 상한이 먼저 소진돼 응답의 꼬리가 영구 유실된다.
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

// settleOnce는 한 시도를 시작해 outcome_unknown으로 정산하고 남은 attempts를 돌려준다.
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

// TestStageRejectsALateLowerOrdinal은 더 높은 ordinal이 이미 있는 (message, phase)에 낮은
// 순번이 뒤늦게 도착하면 받지 않는지 확인한다. 받으면 이미 보낸 응답 뒤에 앞 순번이 다시 나간다.
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

	// 이미 있는 행의 재stage는 가드와 무관하게 멱등해야 한다.
	if outcome, restageErr := store.Stage(ctx, second); restageErr != nil || outcome != irisdurable.ReplyAlreadyStaged {
		t.Fatalf("restage ordinal 1 = (%q, %v); want already_staged", outcome, restageErr)
	}
}

// TestStageRowReturnsTheStoredRow는 stage 직후 저장본을 추가 왕복 없이 읽을 수 있는지 확인한다.
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

// requireSameJSON은 jsonb 왕복이 키 순서와 공백을 정규화하므로 바이트가 아니라 값을 비교한다.
func requireSameJSON(t *testing.T, got, want []byte, label string) {
	t.Helper()

	var gotValue, wantValue any

	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("%s: decode %s: %v", label, got, err)
	}

	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("%s: decode the expected %s: %v", label, want, err)
	}

	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("%s = %s; want %s", label, got, want)
	}
}

// TestCompleteRequiresTheMessageIdAndALiveLease는 종단 전이의 fence를 확인한다. 토큰만 대조하면
// lease가 만료된 뒤 ReclaimInbox 전의 창에서 complete이 성공하고, 같은 순간 reclaim이 행을
// 되돌리면 같은 메시지가 두 번 처리된다.
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

// TestCompleteRecordsANonSuccessReason은 "재시도해도 같은 결과인 입력 결함"처럼 성공이 아닌
// 완료의 사유가 종단 행에 남는지 확인한다.
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

	if status != "completed" || reason == nil || *reason != "invalid_payload" {
		t.Fatalf("completed row = (%q, %v); want completed with the recorded reason", status, reason)
	}
}

// TestDeferReturnsTheAttemptBudget은 처리를 시작하지 못하고 소유권만 반납한 claim이 재시도
// 예산을 쓰지 않는지 확인한다.
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

// TestPruneInboxKeepsManualReviewLonger는 두 종단 갈래의 보존이 따로 도는지 확인한다.
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
		InboxManualReviewRetention: irisdurable.AutomaticReplayHorizon,
	})

	pruned, err = withRetention.PruneInbox(ctx, 10)
	if err != nil {
		t.Fatalf("prune with a manual review retention: %v", err)
	}

	if pruned != 1 {
		t.Fatalf("pruned %d rows; want the manual review row once its retention is set", pruned)
	}
}

// TestManualReviewRetentionMayNotUndercutTerminalRetention은 아직 검토하지 않은 행이 이미
// 검토가 끝난 completed 행보다 먼저 사라지는 구성을 기동 시점에 막는지 본다.
func TestManualReviewRetentionMayNotUndercutTerminalRetention(t *testing.T) {
	t.Parallel()

	_, err := pgstore.New(&pgxpool.Pool{}, pgstore.Options{
		InboxManualReviewRetention: irisdurable.AutomaticReplayHorizon - time.Hour,
	})
	if err == nil {
		t.Fatal("New with a manual review retention shorter than the terminal retention must fail")
	}
}

// TestSnapshotsMatchTheClaimPredicates는 관측 조회가 실제로 집을 수 있는 행을 세는지 확인한다.
// 술어가 어긋나면 밀린 건수와 실제 처리 가능한 행 수가 달라져 워커 수 판단이 틀어진다.
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

// admitAndClaim은 메시지 하나를 admit하고 곧바로 claim해 돌려준다.
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

// TestStageSerializesTheOrdinalSequence는 같은 (message, phase)의 stage가 직렬화되는지 확인한다.
// 직렬화가 없으면 두 stage가 서로의 미commit 행을 보지 못해 후속 ordinal 가드를 양쪽 다
// 통과하고, 늦게 도착한 낮은 순번이 행으로 남아 이미 보낸 응답 뒤에 앞 순번이 다시 나간다.
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

// beginStagedTx는 열린 트랜잭션 안에서 응답 하나를 stage하고 그 트랜잭션을 돌려준다.
func beginStagedTx(t *testing.T, pool *pgxpool.Pool, scope string, record irisdurable.ReplyRecord) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	t.Cleanup(func() {
		//nolint:usetesting // rollback은 commit 여부와 무관하게 돌아야 하므로 이미 취소된 t.Context()를 쓸 수 없다.
		if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", rollbackErr)
		}
	})

	store, err := pgstore.New(tx, pgstore.Options{Scope: scope})
	if err != nil {
		t.Fatalf("new store on the transaction: %v", err)
	}

	if _, stageErr := store.Stage(t.Context(), record); stageErr != nil {
		t.Fatalf("stage inside the open transaction: %v", stageErr)
	}

	return tx
}

// stageAsync는 별도 goroutine에서 stage를 시작하고 결과 채널을 돌려준다. 같은 순번열의 앞
// 트랜잭션이 열려 있는 동안 이 호출은 막혀 있어야 한다.
func stageAsync(store *pgstore.Store, record irisdurable.ReplyRecord) <-chan stageResult {
	done := make(chan stageResult, 1)

	go func() {
		// 이 goroutine은 앞 트랜잭션이 끝날 때까지 살아 있어야 하므로 테스트 컨텍스트를 쓰지 않는다.
		staged, err := store.Stage(context.Background(), record)
		done <- stageResult{staged: staged, err: err}
	}()

	return done
}

// TestRetireReturnsTheRowsItClosed는 Retire가 지우기 직전의 상태와 payload를 함께 돌려주는지
// 확인한다. 전송을 시작하지 못한 행에서 text 대체본을 파생하는 호출자는 이 둘이 없으면 이미
// 전달됐을 수 있는 행까지 다시 보내거나, payload가 지워진 뒤라 아무것도 파생하지 못한다.
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

	retired, err := exhausted.Retire(ctx, 10)
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

// TestClaimPlanUsesThePartialIndexes는 claim과 runtime snapshot이 참조 스키마의 부분 인덱스를
// 실제로 타는지 확인한다. 인덱스를 놓치면 inbox가 커질수록 claim이 순차 스캔으로 내려앉는다.
func TestClaimPlanUsesThePartialIndexes(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire plan connection: %v", err)
	}
	defer conn.Release()

	// 빈 테이블에서는 순차 스캔이 언제나 더 싸므로, 인덱스를 쓸 수 있는지 자체를 본다.
	if _, err := conn.Exec(ctx, `SET enable_seqscan=off`); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}

	scope := store.Options().Scope

	claimPlan := explainQuery(ctx, t, conn, "claim", pgstore.ClaimInboxQueryForTest, scope, "plan-token", float64(60))

	for _, index := range []string{"idx_iris_webhook_inbox_claim", "idx_iris_webhook_inbox_head"} {
		if !strings.Contains(claimPlan, index) {
			t.Fatalf("claim plan does not use %s:\n%s", index, claimPlan)
		}
	}

	runtimePlan := explainQuery(ctx, t, conn, "runtime snapshot", pgstore.InboxRuntimeSnapshotQueryForTest, scope)
	if !strings.Contains(runtimePlan, "idx_iris_webhook_inbox_prune_manual_review") {
		t.Fatalf("runtime snapshot plan does not use the manual-review index:\n%s", runtimePlan)
	}

	// 활성 행 부분 인덱스는 claim과 head 둘 다이고, 어느 쪽을 고르는지는 planner의 몫이다.
	if !strings.Contains(runtimePlan, "idx_iris_webhook_inbox_claim") && !strings.Contains(runtimePlan, "idx_iris_webhook_inbox_head") {
		t.Fatalf("runtime snapshot plan does not use an active-row partial index:\n%s", runtimePlan)
	}
}

func explainQuery(ctx context.Context, t *testing.T, conn *pgxpool.Conn, label, query string, args ...any) string {
	t.Helper()

	rows, err := conn.Query(ctx, "EXPLAIN (COSTS OFF) "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", label, err)
	}
	defer rows.Close()

	var lines []string

	for rows.Next() {
		var line string

		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan %s plan row: %v", label, err)
		}

		lines = append(lines, line)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("read %s plan: %v", label, err)
	}

	return strings.Join(lines, "\n")
}

// TestCountRepliesByStatusLeavesOutTerminalRows는 상태별 개수가 backlog만 담는지 확인한다.
// 종단 행까지 세면 그 값은 backlog가 아니게 되고, 술어가 부분 인덱스를 벗어나 보존 기간
// 전체를 훑는다.
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
