package pgstore

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed queries/*.sql
var queryFS embed.FS

func inboxQuery(name string) pgxdb.Statement {
	return pgxdb.MustStatement(queryFS, name).
		With("{{heads}}", pgxdb.MustStatement(queryFS, "queries/inbox_heads.sql")).
		With("{{eligible}}", pgxdb.MustStatement(queryFS, "queries/inbox_eligible.sql")).
		With("{{eligible_at}}", pgxdb.MustStatement(queryFS, "queries/inbox_eligible_at.sql")).
		WithToken("{{row}}", "current_row")
}

func replyQuery(name string) pgxdb.Statement {
	return pgxdb.MustStatement(queryFS, name).
		With("{{eligible}}", pgxdb.MustStatement(queryFS, "queries/reply_eligible.sql"))
}

func replyEligibleAt() pgxdb.Statement {
	return pgxdb.MustStatement(queryFS, "queries/reply_eligible_at.sql")
}

var (
	admitInboxSQL             = pgxdb.MustStatement(queryFS, "queries/admit_inbox.sql")
	claimInboxSQL             = inboxQuery("queries/claim_inbox.sql").WithToken("{{explicit_recovery}}", "$5")
	completeInboxSQL          = pgxdb.MustStatement(queryFS, "queries/complete_inbox.sql")
	renewInboxLeaseSQL        = pgxdb.MustStatement(queryFS, "queries/renew_inbox_lease.sql")
	releaseInboxSQL           = pgxdb.MustStatement(queryFS, "queries/release_inbox.sql")
	deferInboxSQL             = pgxdb.MustStatement(queryFS, "queries/defer_inbox.sql")
	manualReviewInboxSQL      = pgxdb.MustStatement(queryFS, "queries/manual_review_inbox.sql")
	pruneInboxCompletedSQL    = pgxdb.MustStatement(queryFS, "queries/prune_inbox_completed.sql")
	pruneInboxManualReviewSQL = pgxdb.MustStatement(queryFS, "queries/prune_inbox_manual_review.sql")
	releaseClaimTokenSQL      = pgxdb.MustStatement(queryFS, "queries/release_claim_token.sql")
	inboxRuntimeSnapshotSQL   = pgxdb.MustStatement(queryFS, "queries/inbox_runtime_snapshot.sql")
	inboxReadySnapshotSQL     = inboxQuery("queries/inbox_ready_snapshot.sql").WithToken("{{explicit_recovery}}", "$2")
	applyInboxRecoverySQL     = pgxdb.MustStatement(queryFS, "queries/apply_inbox_recovery.sql")

	insertNonceSQL = pgxdb.MustStatement(queryFS, "queries/insert_nonce.sql")
	pruneNonceSQL  = pgxdb.MustStatement(queryFS, "queries/prune_nonce.sql")

	lockReplySequenceSQL          = pgxdb.MustStatement(queryFS, "queries/lock_reply_sequence.sql")
	stageReplySQL                 = pgxdb.MustStatement(queryFS, "queries/stage_reply.sql")
	markReplyPayloadDivergenceSQL = pgxdb.MustStatement(queryFS, "queries/mark_reply_payload_divergence.sql")
	beginReplyAttemptSQL          = replyQuery("queries/begin_reply_attempt.sql").
					WithToken("{{row}}", "outbox").WithToken("{{max_attempts}}", "$7").WithToken("{{replay_horizon}}", "$8")
	replyExistsSQL  = pgxdb.MustStatement(queryFS, "queries/reply_exists.sql")
	settleReplySQL  = pgxdb.MustStatement(queryFS, "queries/settle_reply.sql")
	inspectReplySQL = pgxdb.MustStatement(queryFS, "queries/inspect_reply.sql")

	listRedrivableRepliesSQL = replyQuery("queries/list_redrivable_replies.sql").
					WithToken("{{row}}", "candidate").WithToken("{{max_attempts}}", "$2").WithToken("{{replay_horizon}}", "$3")
	retireExhaustedRepliesSQL = pgxdb.MustStatement(queryFS, "queries/retire_exhausted_replies.sql")
	pruneRepliesSQL           = pgxdb.MustStatement(queryFS, "queries/prune_replies.sql")
	countRepliesByStatusSQL   = pgxdb.MustStatement(queryFS, "queries/count_replies_by_status.sql")
	replyReadySnapshotSQL     = replyQuery("queries/reply_ready_snapshot.sql").With("{{eligible_at}}", replyEligibleAt()).
					WithToken("{{row}}", "candidate").WithToken("{{max_attempts}}", "$2").WithToken("{{replay_horizon}}", "$3")
)
