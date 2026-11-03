package pgstore

import (
	"embed"
	"strings"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql
var queryFS embed.FS

// 초기화 때 한 번 조합한다. PostgreSQL이 잠근 최신 행을 재검사하도록
// 획득 쿼리의 직접 행 잠금·UPDATE 조건은 유지한다.
func inboxQuery(name, explicitRecovery string) string {
	query := strings.NewReplacer(
		"{{heads}}", sqlutil.MustQuery(queryFS, "queries/inbox_heads.sql"),
		"{{eligible}}", sqlutil.MustQuery(queryFS, "queries/inbox_eligible.sql"),
		"{{eligible_at}}", sqlutil.MustQuery(queryFS, "queries/inbox_eligible_at.sql"),
	).Replace(sqlutil.MustQuery(queryFS, name))

	return strings.NewReplacer("{{row}}", "current_row", "{{explicit_recovery}}", explicitRecovery).Replace(query)
}

func replyQuery(name, row, maxAttempts, replayHorizon string) string {
	query := strings.NewReplacer(
		"{{eligible}}", sqlutil.MustQuery(queryFS, "queries/reply_eligible.sql"),
		"{{eligible_at}}", sqlutil.MustQuery(queryFS, "queries/reply_eligible_at.sql"),
	).Replace(sqlutil.MustQuery(queryFS, name))

	return strings.NewReplacer("{{row}}", row, "{{max_attempts}}", maxAttempts, "{{replay_horizon}}", replayHorizon).Replace(query)
}

var (
	admitInboxSQL             = sqlutil.MustQuery(queryFS, "queries/admit_inbox.sql")
	claimInboxSQL             = inboxQuery("queries/claim_inbox.sql", "$5")
	completeInboxSQL          = sqlutil.MustQuery(queryFS, "queries/complete_inbox.sql")
	renewInboxLeaseSQL        = sqlutil.MustQuery(queryFS, "queries/renew_inbox_lease.sql")
	releaseInboxSQL           = sqlutil.MustQuery(queryFS, "queries/release_inbox.sql")
	deferInboxSQL             = sqlutil.MustQuery(queryFS, "queries/defer_inbox.sql")
	manualReviewInboxSQL      = sqlutil.MustQuery(queryFS, "queries/manual_review_inbox.sql")
	pruneInboxCompletedSQL    = sqlutil.MustQuery(queryFS, "queries/prune_inbox_completed.sql")
	pruneInboxManualReviewSQL = sqlutil.MustQuery(queryFS, "queries/prune_inbox_manual_review.sql")
	releaseClaimTokenSQL      = sqlutil.MustQuery(queryFS, "queries/release_claim_token.sql")
	inboxRuntimeSnapshotSQL   = sqlutil.MustQuery(queryFS, "queries/inbox_runtime_snapshot.sql")
	inboxReadySnapshotSQL     = inboxQuery("queries/inbox_ready_snapshot.sql", "$2")
	applyInboxRecoverySQL     = sqlutil.MustQuery(queryFS, "queries/apply_inbox_recovery.sql")

	insertNonceSQL = sqlutil.MustQuery(queryFS, "queries/insert_nonce.sql")
	pruneNonceSQL  = sqlutil.MustQuery(queryFS, "queries/prune_nonce.sql")

	lockReplySequenceSQL          = sqlutil.MustQuery(queryFS, "queries/lock_reply_sequence.sql")
	stageReplySQL                 = sqlutil.MustQuery(queryFS, "queries/stage_reply.sql")
	markReplyPayloadDivergenceSQL = sqlutil.MustQuery(queryFS, "queries/mark_reply_payload_divergence.sql")
	beginReplyAttemptSQL          = replyQuery("queries/begin_reply_attempt.sql", "outbox", "$7", "$8")
	replyExistsSQL                = sqlutil.MustQuery(queryFS, "queries/reply_exists.sql")
	settleReplySQL                = sqlutil.MustQuery(queryFS, "queries/settle_reply.sql")
	inspectReplySQL               = sqlutil.MustQuery(queryFS, "queries/inspect_reply.sql")

	listRedrivableRepliesSQL  = replyQuery("queries/list_redrivable_replies.sql", "candidate", "$2", "$3")
	retireExhaustedRepliesSQL = sqlutil.MustQuery(queryFS, "queries/retire_exhausted_replies.sql")
	pruneRepliesSQL           = sqlutil.MustQuery(queryFS, "queries/prune_replies.sql")
	countRepliesByStatusSQL   = sqlutil.MustQuery(queryFS, "queries/count_replies_by_status.sql")
	replyReadySnapshotSQL     = replyQuery("queries/reply_ready_snapshot.sql", "candidate", "$2", "$3")
)
