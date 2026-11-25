package pgstore

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql
var queryFS embed.FS

func inboxQuery(name, explicitRecovery string) pgxdb.Statement {
	return pgxdb.MustStatement(queryFS, name).Replace(
		"{{heads}}", sqlutil.MustQuery(queryFS, "queries/inbox_heads.sql"),
		"{{eligible}}", sqlutil.MustQuery(queryFS, "queries/inbox_eligible.sql"),
		"{{eligible_at}}", sqlutil.MustQuery(queryFS, "queries/inbox_eligible_at.sql"),
	).Replace("{{row}}", "current_row", "{{explicit_recovery}}", explicitRecovery)
}

func replyQuery(name, row, maxAttempts, replayHorizon string) pgxdb.Statement {
	return pgxdb.MustStatement(queryFS, name).Replace(
		"{{eligible}}", sqlutil.MustQuery(queryFS, "queries/reply_eligible.sql"),
		"{{eligible_at}}", sqlutil.MustQuery(queryFS, "queries/reply_eligible_at.sql"),
	).Replace("{{row}}", row, "{{max_attempts}}", maxAttempts, "{{replay_horizon}}", replayHorizon)
}

var (
	admitInboxSQL             = pgxdb.MustStatement(queryFS, "queries/admit_inbox.sql")
	claimInboxSQL             = inboxQuery("queries/claim_inbox.sql", "$5")
	completeInboxSQL          = pgxdb.MustStatement(queryFS, "queries/complete_inbox.sql")
	renewInboxLeaseSQL        = pgxdb.MustStatement(queryFS, "queries/renew_inbox_lease.sql")
	releaseInboxSQL           = pgxdb.MustStatement(queryFS, "queries/release_inbox.sql")
	deferInboxSQL             = pgxdb.MustStatement(queryFS, "queries/defer_inbox.sql")
	manualReviewInboxSQL      = pgxdb.MustStatement(queryFS, "queries/manual_review_inbox.sql")
	pruneInboxCompletedSQL    = pgxdb.MustStatement(queryFS, "queries/prune_inbox_completed.sql")
	pruneInboxManualReviewSQL = pgxdb.MustStatement(queryFS, "queries/prune_inbox_manual_review.sql")
	releaseClaimTokenSQL      = pgxdb.MustStatement(queryFS, "queries/release_claim_token.sql")
	inboxRuntimeSnapshotSQL   = pgxdb.MustStatement(queryFS, "queries/inbox_runtime_snapshot.sql")
	inboxReadySnapshotSQL     = inboxQuery("queries/inbox_ready_snapshot.sql", "$2")
	applyInboxRecoverySQL     = pgxdb.MustStatement(queryFS, "queries/apply_inbox_recovery.sql")

	insertNonceSQL = pgxdb.MustStatement(queryFS, "queries/insert_nonce.sql")
	pruneNonceSQL  = pgxdb.MustStatement(queryFS, "queries/prune_nonce.sql")

	lockReplySequenceSQL          = pgxdb.MustStatement(queryFS, "queries/lock_reply_sequence.sql")
	stageReplySQL                 = pgxdb.MustStatement(queryFS, "queries/stage_reply.sql")
	markReplyPayloadDivergenceSQL = pgxdb.MustStatement(queryFS, "queries/mark_reply_payload_divergence.sql")
	beginReplyAttemptSQL          = replyQuery("queries/begin_reply_attempt.sql", "outbox", "$7", "$8")
	replyExistsSQL                = pgxdb.MustStatement(queryFS, "queries/reply_exists.sql")
	settleReplySQL                = pgxdb.MustStatement(queryFS, "queries/settle_reply.sql")
	inspectReplySQL               = pgxdb.MustStatement(queryFS, "queries/inspect_reply.sql")

	listRedrivableRepliesSQL  = replyQuery("queries/list_redrivable_replies.sql", "candidate", "$2", "$3")
	retireExhaustedRepliesSQL = pgxdb.MustStatement(queryFS, "queries/retire_exhausted_replies.sql")
	pruneRepliesSQL           = pgxdb.MustStatement(queryFS, "queries/prune_replies.sql")
	countRepliesByStatusSQL   = pgxdb.MustStatement(queryFS, "queries/count_replies_by_status.sql")
	replyReadySnapshotSQL     = replyQuery("queries/reply_ready_snapshot.sql", "candidate", "$2", "$3")
)
