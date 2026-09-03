package pgstore

import (
	"embed"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql
var queryFS embed.FS

func mustQuery(name string) string {
	return sqlutil.MustQuery(queryFS, name)
}

var (
	queryAdmitInbox           = mustQuery("queries/admit_inbox.sql")
	queryClaimInbox           = mustQuery("queries/claim_inbox.sql")
	queryCompleteInbox        = mustQuery("queries/complete_inbox.sql")
	queryRenewInboxLease      = mustQuery("queries/renew_inbox_lease.sql")
	queryReleaseInbox         = mustQuery("queries/release_inbox.sql")
	queryDeferInbox           = mustQuery("queries/defer_inbox.sql")
	queryManualReviewInbox    = mustQuery("queries/manual_review_inbox.sql")
	queryReclaimInbox         = mustQuery("queries/reclaim_inbox.sql")
	queryPruneInbox           = mustQuery("queries/prune_inbox.sql")
	queryInboxRuntimeSnapshot = mustQuery("queries/inbox_runtime_snapshot.sql")
	queryInboxReadySnapshot   = mustQuery("queries/inbox_ready_snapshot.sql")

	queryInsertNonce = mustQuery("queries/insert_nonce.sql")
	queryPruneNonce  = mustQuery("queries/prune_nonce.sql")

	queryLockReplySequence          = mustQuery("queries/lock_reply_sequence.sql")
	queryStageReply                 = mustQuery("queries/stage_reply.sql")
	queryMarkReplyPayloadDivergence = mustQuery("queries/mark_reply_payload_divergence.sql")
	queryBeginReplyAttempt          = mustQuery("queries/begin_reply_attempt.sql")
	queryRenewReplyLease            = mustQuery("queries/renew_reply_lease.sql")
	queryReplyExists                = mustQuery("queries/reply_exists.sql")
	querySettleReply                = mustQuery("queries/settle_reply.sql")
	queryManualReviewReply          = mustQuery("queries/manual_review_reply.sql")
	queryInspectReply               = mustQuery("queries/inspect_reply.sql")

	queryListRedrivableReplies  = mustQuery("queries/list_redrivable_replies.sql")
	queryRetireExhaustedReplies = mustQuery("queries/retire_exhausted_replies.sql")
	queryPruneReplies           = mustQuery("queries/prune_replies.sql")
	queryCountRepliesByStatus   = mustQuery("queries/count_replies_by_status.sql")
	queryReplyReadySnapshot     = mustQuery("queries/reply_ready_snapshot.sql")
)
