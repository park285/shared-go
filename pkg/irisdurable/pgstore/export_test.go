package pgstore

// 실행 계획 검증 테스트가 실제로 도는 문을 EXPLAIN할 수 있도록 내보낸다. _test.go 파일이므로
// 공개 API는 넓어지지 않는다.
var (
	ClaimInboxQueryForTest           = queryClaimInbox
	InboxRuntimeSnapshotQueryForTest = queryInboxRuntimeSnapshot
)
