package pgstore

import (
	"embed"
	"strings"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql
var queryFS embed.FS

func mustQuery(name string) string {
	return sqlutil.MustQuery(queryFS, name)
}

// 초기화 때 한 번 조합한다. PostgreSQL이 잠근 최신 행을 재검사하도록
// 획득 쿼리의 직접 행 잠금·UPDATE 조건은 유지한다.
func inboxQuery(name, explicitRecovery string) string {
	query := strings.NewReplacer(
		"{{heads}}", mustQuery("queries/inbox_heads.sql"),
		"{{eligible}}", mustQuery("queries/inbox_eligible.sql"),
		"{{eligible_at}}", mustQuery("queries/inbox_eligible_at.sql"),
	).Replace(mustQuery(name))

	return strings.NewReplacer("{{row}}", "current_row", "{{explicit_recovery}}", explicitRecovery).Replace(query)
}

func replyQuery(name, row, maxAttempts, replayHorizon string) string {
	query := strings.NewReplacer(
		"{{eligible}}", mustQuery("queries/reply_eligible.sql"),
		"{{eligible_at}}", mustQuery("queries/reply_eligible_at.sql"),
	).Replace(mustQuery(name))

	return strings.NewReplacer("{{row}}", row, "{{max_attempts}}", maxAttempts, "{{replay_horizon}}", replayHorizon).Replace(query)
}

var (
	queryAdmitInbox             = mustQuery("queries/admit_inbox.sql")
	queryClaimInbox             = inboxQuery("queries/claim_inbox.sql", "$5")
	queryCompleteInbox          = mustQuery("queries/complete_inbox.sql")
	queryRenewInboxLease        = mustQuery("queries/renew_inbox_lease.sql")
	queryReleaseInbox           = mustQuery("queries/release_inbox.sql")
	queryDeferInbox             = mustQuery("queries/defer_inbox.sql")
	queryManualReviewInbox      = mustQuery("queries/manual_review_inbox.sql")
	queryPruneInboxCompleted    = mustQuery("queries/prune_inbox_completed.sql")
	queryPruneInboxManualReview = mustQuery("queries/prune_inbox_manual_review.sql")
	queryReleaseClaimToken      = mustQuery("queries/release_claim_token.sql")
	queryInboxRuntimeSnapshot   = mustQuery("queries/inbox_runtime_snapshot.sql")
	queryInboxReadySnapshot     = inboxQuery("queries/inbox_ready_snapshot.sql", "$2")
	queryApplyInboxRecovery     = mustQuery("queries/apply_inbox_recovery.sql")

	queryInsertNonce = mustQuery("queries/insert_nonce.sql")
	queryPruneNonce  = mustQuery("queries/prune_nonce.sql")

	queryLockReplySequence          = mustQuery("queries/lock_reply_sequence.sql")
	queryStageReply                 = mustQuery("queries/stage_reply.sql")
	queryMarkReplyPayloadDivergence = mustQuery("queries/mark_reply_payload_divergence.sql")
	queryBeginReplyAttempt          = replyQuery("queries/begin_reply_attempt.sql", "outbox", "$7", "$8")
	queryReplyExists                = mustQuery("queries/reply_exists.sql")
	querySettleReply                = mustQuery("queries/settle_reply.sql")
	queryInspectReply               = mustQuery("queries/inspect_reply.sql")

	queryListRedrivableReplies  = replyQuery("queries/list_redrivable_replies.sql", "candidate", "$2", "$3")
	queryRetireExhaustedReplies = mustQuery("queries/retire_exhausted_replies.sql")
	queryPruneReplies           = mustQuery("queries/prune_replies.sql")
	queryCountRepliesByStatus   = mustQuery("queries/count_replies_by_status.sql")
	queryReplyReadySnapshot     = replyQuery("queries/reply_ready_snapshot.sql", "candidate", "$2", "$3")
)
