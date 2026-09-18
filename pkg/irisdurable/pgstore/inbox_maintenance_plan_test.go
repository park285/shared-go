package pgstore_test

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

// 기존 ChatBotGo의 10만 행 maintenance 회귀를 SQL 소유자인 pgstore로 이동했다.
func TestInboxMaintenancePlansStayIndexBoundedAtScale(t *testing.T) {
	db := newMigratedPool(t)
	now := time.Now().UTC()

	if _, err := db.Exec(t.Context(), `
		INSERT INTO iris_webhook_inbox (
			scope, message_id, ordering_key, payload, status, attempts,
			available_at, claim_token, lease_until, created_at, updated_at
		)
		SELECT
			'chat', 'plan-' || value, 'plan-order-' || value, '{}'::jsonb,
			CASE value % 3 WHEN 0 THEN 'processing' ELSE 'pending' END,
			1, $1::timestamptz + (value * INTERVAL '1 millisecond'),
 CASE WHEN value % 3 = 0 THEN 'token' ELSE NULL END,
			CASE WHEN value % 3 = 0 THEN $1::timestamptz + (value * INTERVAL '1 millisecond') ELSE NULL END,
			$1::timestamptz + (value * INTERVAL '1 millisecond'), $1::timestamptz
		FROM generate_series(1, 100000) AS value
	`, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("insert maintenance plan fixture: %v", err)
	}

	if _, err := db.Exec(t.Context(), "ANALYZE iris_webhook_inbox"); err != nil {
		t.Fatalf("analyze maintenance plan fixture: %v", err)
	}

	stalePlan := explainMaintenanceQuery(
		t,
		db,
		pgstore.CompleteInboxBeforeQueryForTest,
		"chat", now.Add(-time.Minute), 1000, "inbox age exceeded",
	)
	assertBoundedMaintenancePlan(t, stalePlan, "idx_iris_webhook_inbox_pending_age", 1)

	recoveryPlan := explainMaintenanceQuery(
		t,
		db,
		pgstore.RecoverInboxBeforeQueryForTest,
		"chat", now.Add(-time.Minute), 3, 1000, true,
	)
	assertBoundedMaintenancePlan(t, recoveryPlan, "idx_iris_webhook_inbox_lease", 1)
}

func TestInboxStaleCandidateScansStopAtLimitForEqualTimestamps(t *testing.T) {
	db := newMigratedPool(t)
	now := time.Now().UTC()

	const limit = 1000

	if _, err := db.Exec(t.Context(), `
		INSERT INTO iris_webhook_inbox (
			scope, message_id, ordering_key, payload, status, created_at, updated_at
		)
		SELECT
			'chat', 'equal-time-' || value,
			'equal-order-' || value,
			'{}'::jsonb,
			'pending',
			$1,
			$1
		FROM generate_series(1, 100000) AS value
	`, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("insert equal-timestamp maintenance fixture: %v", err)
	}

	if _, err := db.Exec(t.Context(), "ANALYZE iris_webhook_inbox"); err != nil {
		t.Fatalf("analyze equal-timestamp maintenance fixture: %v", err)
	}

	plan := explainMaintenanceJSON(t, db, pgstore.CompleteInboxBeforeQueryForTest, "chat", now.Add(-time.Minute), limit, "inbox age exceeded")
	assertEqualTimestampStalePlan(t, plan, limit)
}

type explainPlanDocument struct {
	Plan explainPlanNode `json:"Plan"`
}

type explainPlanNode struct {
	NodeType    string            `json:"Node Type"`
	IndexName   string            `json:"Index Name"`
	ActualRows  float64           `json:"Actual Rows"`
	ActualLoops float64           `json:"Actual Loops"`
	Plans       []explainPlanNode `json:"Plans"`
}

func explainMaintenanceJSON(t *testing.T, db *pgxpool.Pool, query string, args ...any) explainPlanNode {
	t.Helper()

	var raw []byte

	if err := db.QueryRow(
		t.Context(),
		"EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY OFF, FORMAT JSON) "+query,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("explain maintenance query as JSON: %v", err)
	}

	var documents []explainPlanDocument

	if err := jsonv2.Unmarshal(raw, &documents); err != nil {
		t.Fatalf("decode maintenance JSON plan: %v", err)
	}

	if len(documents) != 1 {
		t.Fatalf("maintenance plan documents = %d, want 1", len(documents))
	}

	return documents[0].Plan
}

func assertEqualTimestampStalePlan(t *testing.T, plan explainPlanNode, limit int) {
	t.Helper()

	var candidateScans int

	visitExplainPlan(plan, func(node explainPlanNode) {
		if node.NodeType == "Incremental Sort" {
			t.Fatalf("equal-timestamp stale plan contains Incremental Sort: %#v", node)
		}

		if node.NodeType == "Sort" && node.ActualRows*node.ActualLoops > float64(2*limit) {
			t.Fatalf("equal-timestamp stale sort rows x loops = %.0f, want <= %d", node.ActualRows*node.ActualLoops, 2*limit)
		}

		if node.IndexName != "idx_iris_webhook_inbox_pending_age" {
			return
		}

		candidateScans++
		t.Logf(
			"candidate index scan %d actual_rows=%.0f actual_loops=%.0f rows_x_loops=%.0f",
			candidateScans,
			node.ActualRows,
			node.ActualLoops,
			node.ActualRows*node.ActualLoops,
		)

		if scanned := node.ActualRows * node.ActualLoops; scanned > float64(limit) {
			t.Fatalf("candidate index rows x loops = %.0f, want <= %d", scanned, limit)
		}
	})

	if candidateScans != 1 {
		t.Fatalf("candidate index scans = %d, want one pending scan", candidateScans)
	}
}

func visitExplainPlan(node explainPlanNode, visit func(explainPlanNode)) {
	visit(node)

	for _, child := range node.Plans {
		visitExplainPlan(child, visit)
	}
}

func explainMaintenanceQuery(t *testing.T, db *pgxpool.Pool, query string, args ...any) string {
	t.Helper()

	rows, err := db.Query(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY OFF) "+query, args...)
	if err != nil {
		t.Fatalf("explain maintenance query: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var line string

		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan maintenance plan: %v", err)
		}

		plan.WriteString(line)
		plan.WriteByte('\n')
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate maintenance plan: %v", err)
	}

	return plan.String()
}

func assertBoundedMaintenancePlan(t *testing.T, plan, candidateIndex string, minimumCandidateScans int) {
	t.Helper()

	if got := strings.Count(plan, candidateIndex); got < minimumCandidateScans {
		t.Fatalf("maintenance plan uses %s %d times, want at least %d:\n%s", candidateIndex, got, minimumCandidateScans, plan)
	}

	if !strings.Contains(plan, "iris_webhook_inbox_pkey") {
		t.Fatalf("maintenance plan lacks bounded primary-key lookup:\n%s", plan)
	}

	for _, forbidden := range []string{"Seq Scan on iris_webhook_inbox", "external merge"} {
		if strings.Contains(plan, forbidden) {
			t.Fatalf("maintenance plan contains %q:\n%s", forbidden, plan)
		}
	}
}
