package pgstore_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

const (
	pruneScope      = "chat"
	pruneOtherScope = "prune-neighbor"
	pruneLimit      = 7
	prunePlannerOff = "off"
	// 각 scope에 넣는 오래된 종단 행 수와 그중 manual_review 수(v % 4 = 0)다.
	pruneOldRows    = 160
	pruneReviewRows = pruneOldRows / 4
	// 젊은 completed, pending, processing 행을 종류마다 이만큼 넣는다.
	pruneProtectedRows = 5
)

// rescanningPlanner는 후보 부분 질의를 nested loop 안쪽에 두고 바깥 행마다 다시 실행하는 계획을
// planner가 고르게 한다. 통계와 규모에 따라 운영 planner도 고를 수 있는 모양이므로, 호출당
// 상한은 어떤 조인 전략에서도 지켜져야 한다.
var rescanningPlanner = map[string]string{
	"enable_hashjoin":  prunePlannerOff,
	"enable_mergejoin": prunePlannerOff,
	"enable_hashagg":   prunePlannerOff,
	"enable_material":  prunePlannerOff,
	"enable_sort":      prunePlannerOff,
}

// TestPruneInboxDeletesAtMostTheLimitPerCall은 지울 행이 많아도 한 호출이 요청한 수를 넘겨 지우지
// 않고, 매번 남은 후보 중 (terminal_at, id)가 가장 앞선 행만 지우는지 확인한다. 넘겨 지우면
// 유지보수 한 번의 잠금·WAL·지연이 batch 크기로 묶이지 않는다.
func TestPruneInboxDeletesAtMostTheLimitPerCall(t *testing.T) {
	planners := []struct {
		name   string
		params map[string]string
	}{
		{name: "default planner"},
		{name: "rescanning nested loop", params: rescanningPlanner},
	}

	reviews := []struct {
		name             string
		retention        time.Duration
		survivingReviews int
	}{
		{name: "manual review kept", survivingReviews: pruneReviewRows},
		{name: "manual review expires", retention: irisdurable.AutomaticReplayHorizon},
	}

	for _, planner := range planners {
		for _, review := range reviews {
			t.Run(planner.name+"/"+review.name, func(t *testing.T) {
				pool := newMigratedPool(t)
				base := seedPruneFixture(t, pool)
				store := newScopedStoreWithOptions(t, withPlanner(t, pool, planner.params), pgstore.Options{
					Scope: pruneScope, InboxManualReviewRetention: review.retention,
				})
				includeReview := review.retention > 0
				cutoff := base.Add(60 * time.Second)

				drainPrune(t, pool, pruneOracle(t, pool, includeReview, cutoff), func() (int64, error) {
					return store.PruneInboxBefore(t.Context(), cutoff, pruneLimit)
				})

				drainPrune(t, pool, pruneOracle(t, pool, includeReview, time.Now()), func() (int64, error) {
					return store.PruneInbox(t.Context(), pruneLimit)
				})

				assertPruneSurvivors(t, pool, review.survivingReviews)
			})
		}
	}
}

// TestPruneInboxSkipsLockedRowsWithinTheLimit은 다른 트랜잭션이 잡은 후보를 건너뛰고 그 뒤 후보로
// 상한을 채우는지 확인한다. 잠긴 행에서 멈추거나 기다리면 동시에 도는 유지보수가 서로를 막는다.
func TestPruneInboxSkipsLockedRowsWithinTheLimit(t *testing.T) {
	pool := newMigratedPool(t)
	seedPruneFixture(t, pool)

	store := newScopedStoreWithOptions(t, withPlanner(t, pool, rescanningPlanner), pgstore.Options{Scope: pruneScope})
	ctx := t.Context()

	eligible := pruneOracle(t, pool, false, time.Now())
	locked := eligible[:3]

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the locking transaction: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback the locking transaction: %v", rollbackErr)
		}
	})

	if _, lockErr := tx.Exec(ctx, `SELECT id FROM iris_webhook_inbox WHERE id = ANY($1) FOR UPDATE`, locked); lockErr != nil {
		t.Fatalf("lock the oldest candidates: %v", lockErr)
	}

	before := inboxIDs(t, pool)

	pruned, err := store.PruneInbox(ctx, pruneLimit)
	if err != nil {
		t.Fatalf("prune past locked rows: %v", err)
	}

	deleted := removedIDs(before, inboxIDs(t, pool))
	want := slices.Sorted(slices.Values(eligible[len(locked) : len(locked)+pruneLimit]))

	if pruned != pruneLimit || !slices.Equal(deleted, want) {
		t.Fatalf("prune with %v locked deleted %v (reported %d); want the next %d candidates %v", locked, deleted, pruned, pruneLimit, want)
	}

	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("release the locked candidates: %v", rollbackErr)
	}

	drainPrune(t, pool, pruneOracle(t, pool, false, time.Now()), func() (int64, error) {
		return store.PruneInbox(ctx, pruneLimit)
	})

	assertPruneSurvivors(t, pool, pruneReviewRows)
}

// seedPruneFixture는 두 scope에 같은 모양의 오래된 종단 행을 섞어 넣고, pruneScope에만 보존이
// 남은 completed와 활성 행을 더한다. 각 terminal_at은 두 행씩 같아 id가 순서를 가른다. 기준 시각을
// 반환한다.
func seedPruneFixture(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	base := time.Now().Add(-400 * 24 * time.Hour).UTC().Truncate(time.Second)

	const terminal = `
		INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, terminal_at, terminal_reason)
		SELECT s.scope,
		       CASE WHEN v % 4 = 0 THEN 'old-review-' ELSE 'old-completed-' END || v,
		       'room-' || v,
		       '{}'::jsonb,
		       CASE WHEN v % 4 = 0 THEN 'manual_review' ELSE 'completed' END,
		       $3::timestamptz + (v / 2) * interval '1 second',
		       CASE WHEN v % 4 = 0 THEN 'needs a human' END
		FROM generate_series(1, $4::int) AS v, (VALUES ($1::text), ($2::text)) AS s(scope)`

	if _, err := pool.Exec(t.Context(), terminal, pruneScope, pruneOtherScope, base, pruneOldRows); err != nil {
		t.Fatalf("seed old terminal rows: %v", err)
	}

	const protected = `
		INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, claim_token, lease_until, terminal_at)
		SELECT $1, k.kind || '-' || v, k.kind || '-room-' || v, '{}'::jsonb,
		       CASE k.kind WHEN 'young' THEN 'completed' ELSE k.kind END,
		       CASE k.kind WHEN 'processing' THEN 'token' END,
		       CASE k.kind WHEN 'processing' THEN now() + interval '1 hour' END,
		       CASE k.kind WHEN 'young' THEN now() - interval '1 hour' END
		FROM generate_series(1, $2::int) AS v, (VALUES ('young'), ('pending'), ('processing')) AS k(kind)`

	if _, err := pool.Exec(t.Context(), protected, pruneScope, pruneProtectedRows); err != nil {
		t.Fatalf("seed protected rows: %v", err)
	}

	return base
}

// withPlanner는 같은 데이터베이스에 planner 설정만 바꾼 연결을 연다. 설정이 없으면 pool을 그대로 쓴다.
func withPlanner(t *testing.T, pool *pgxpool.Pool, params map[string]string) *pgxpool.Pool {
	t.Helper()

	if len(params) == 0 {
		return pool
	}

	config := pool.Config()
	maps.Copy(config.ConnConfig.RuntimeParams, params)

	planned, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("connect with planner settings %v: %v", params, err)
	}

	t.Cleanup(planned.Close)

	return planned
}

// pruneOracle은 fixture 이름표로 pruneScope의 남은 후보를 기대 삭제 순서대로 돌려준다.
func pruneOracle(t *testing.T, pool *pgxpool.Pool, includeReview bool, cutoff time.Time) []int64 {
	t.Helper()

	const query = `
		SELECT id FROM iris_webhook_inbox
		WHERE scope = $1
		  AND (message_id LIKE 'old-completed-%' OR ($2 AND message_id LIKE 'old-review-%'))
		  AND terminal_at <= $3
		ORDER BY terminal_at, id`

	rows, err := pool.Query(t.Context(), query, pruneScope, includeReview, cutoff)
	if err != nil {
		t.Fatalf("query prune candidates: %v", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatalf("collect prune candidates: %v", err)
	}

	return ids
}

// drainPrune은 prune을 빈 호출까지 반복하며 매 호출이 남은 후보 중 앞선 pruneLimit개만 지우는지
// 본다. 차집합을 테이블 전체에서 구하므로 다른 scope나 보호 행을 지워도 드러난다.
func drainPrune(t *testing.T, pool *pgxpool.Pool, candidates []int64, prune func() (int64, error)) {
	t.Helper()

	for call := 1; ; call++ {
		chunk := candidates[:min(pruneLimit, len(candidates))]
		before := inboxIDs(t, pool)

		pruned, err := prune()
		if err != nil {
			t.Fatalf("prune call %d: %v", call, err)
		}

		deleted := removedIDs(before, inboxIDs(t, pool))
		want := slices.Sorted(slices.Values(chunk))

		if pruned != int64(len(want)) || !slices.Equal(deleted, want) {
			t.Fatalf("prune call %d with limit %d deleted %d rows %v (reported %d); want the oldest candidates %v",
				call, pruneLimit, len(deleted), deleted, pruned, want)
		}

		if len(chunk) == 0 {
			return
		}

		candidates = candidates[len(chunk):]
	}
}

// assertPruneSurvivors는 prune이 끝난 뒤에도 보호 대상 행이 모두 남았는지 본다.
func assertPruneSurvivors(t *testing.T, pool *pgxpool.Pool, wantReview int) {
	t.Helper()

	const query = `
		SELECT count(*) FILTER (WHERE scope = $1 AND message_id LIKE 'old-completed-%'),
		       count(*) FILTER (WHERE scope = $1 AND message_id LIKE 'old-review-%'),
		       count(*) FILTER (WHERE scope = $1 AND message_id LIKE 'young-%'),
		       count(*) FILTER (WHERE scope = $1 AND status = 'pending'),
		       count(*) FILTER (WHERE scope = $1 AND status = 'processing'),
		       count(*) FILTER (WHERE scope = $2)
		FROM iris_webhook_inbox`

	var completed, review, young, pending, processing, neighbor int

	if err := pool.QueryRow(t.Context(), query, pruneScope, pruneOtherScope).Scan(
		&completed, &review, &young, &pending, &processing, &neighbor,
	); err != nil {
		t.Fatalf("count survivors: %v", err)
	}

	if completed != 0 || review != wantReview || young != pruneProtectedRows || pending != pruneProtectedRows ||
		processing != pruneProtectedRows || neighbor != pruneOldRows {
		t.Fatalf("survivors: old completed=%d review=%d young=%d pending=%d processing=%d neighbor=%d; want 0, %d, %d, %d, %d, %d",
			completed, review, young, pending, processing, neighbor,
			wantReview, pruneProtectedRows, pruneProtectedRows, pruneProtectedRows, pruneOldRows)
	}
}

func inboxIDs(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT id FROM iris_webhook_inbox ORDER BY id`)
	if err != nil {
		t.Fatalf("list inbox ids: %v", err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatalf("collect inbox ids: %v", err)
	}

	return ids
}

// removedIDs는 정렬된 before에서 정렬된 after에 없는 id를 오름차순으로 돌려준다.
func removedIDs(before, after []int64) []int64 {
	return slices.DeleteFunc(slices.Clone(before), func(id int64) bool {
		_, kept := slices.BinarySearch(after, id)

		return kept
	})
}
