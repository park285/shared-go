package sqlitestore

import (
	"embed"

	"github.com/park285/shared-go/sqlite/sqlitedb"
)

//go:embed queries/*.sql
var queryFS embed.FS

var (
	admitInboxSQL             = sqlitedb.MustQuery(queryFS, "queries/admit_inbox.sql")
	claimCandidatesSQL        = sqlitedb.MustQuery(queryFS, "queries/claim_candidates.sql")
	claimInboxSQL             = sqlitedb.MustQuery(queryFS, "queries/claim_inbox.sql")
	completeInboxSQL          = sqlitedb.MustQuery(queryFS, "queries/complete_inbox.sql")
	releaseInboxAtSQL         = sqlitedb.MustQuery(queryFS, "queries/release_inbox_at.sql")
	deferInboxUntilSQL        = sqlitedb.MustQuery(queryFS, "queries/defer_inbox_until.sql")
	manualReviewInboxSQL      = sqlitedb.MustQuery(queryFS, "queries/manual_review_inbox.sql")
	releaseClaimTokenSQL      = sqlitedb.MustQuery(queryFS, "queries/release_claim_token.sql")
	applyInboxRecoverySQL     = sqlitedb.MustQuery(queryFS, "queries/apply_inbox_recovery.sql")
	inboxReadySnapshotSQL     = sqlitedb.MustQuery(queryFS, "queries/inbox_ready_snapshot.sql")
	pruneInboxCompletedSQL    = sqlitedb.MustQuery(queryFS, "queries/prune_inbox_completed.sql")
	pruneInboxManualReviewSQL = sqlitedb.MustQuery(queryFS, "queries/prune_inbox_manual_review.sql")

	insertNonceSQL = sqlitedb.MustQuery(queryFS, "queries/insert_nonce.sql")
	pruneNonceSQL  = sqlitedb.MustQuery(queryFS, "queries/prune_nonce.sql")
)
