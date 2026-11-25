package pgstore

var (
	ClaimInboxQueryForTest           = claimInboxSQL.SQL()
	InboxReadySnapshotQueryForTest   = inboxReadySnapshotSQL.SQL()
	InboxRuntimeSnapshotQueryForTest = inboxRuntimeSnapshotSQL.SQL()
)
