// Package pgstore는 irisdurable 계약의 PostgreSQL 구현이다.
//
// Store 하나가 Admitter·NonceStore·ReplyOutbox를 모두 제공하며, 상태 기계는 SQL CHECK 제약과
// claim fence로 데이터베이스가 강제한다. 런타임 SQL은 queries/*.sql 자산이 소유하고 sql.go가
// //go:embed로 묶는다.
//
// 테이블 iris_webhook_inbox·iris_nonce·iris_reply_outbox의 DDL은 이 패키지가 소유하지 않는다.
// 스택 SQL 소유권 계약상 schema SQL은 각 소비 저장소의 migration에만 있으므로, 소비자가
// testdata/schema.sql을 자기 migration 규약에 맞춰 옮기고 contracttest 스위트로 스키마가 이
// 구현의 기대와 맞는지 증명한다.
//
// ordering key당 FIFO는 별도 head 테이블 대신 inbox 자신에서 파생한다. Claim은 같은
// ordering key에 더 오래된 미종단 행이 없는 후보만 잡으므로 head가 두 번째 진실이 되지 않는다.
// 파생 규칙은 admit이 직렬화될 때만 성립한다. READ COMMITTED에서 Claim은 아직 commit되지 않은
// 행을 볼 수 없으므로, Admit이 ordering key마다 advisory transaction lock을 잡아 같은 key의
// 삽입 순서와 가시성 순서를 일치시킨다.
//
// Claim과 준비 상태 스냅샷은 같은 적격 조건 SQL을 공유한다.
// 대기 시간의 기준은 available_at 또는 직접 재획득 시 더 늦은 임대 만료 시각이며,
// 선행 행이 차단을 해제했던 과거 시각이 아니다. 스냅샷은 적격한 잠긴 행도 포함하고
// SKIP LOCKED는 Claim에만 적용한다. InboxClaim은 EligibleAt과 원래 CreatedAt을 함께 보존한다.
//
// Retire는 SQL 실행 전에 기존 트랜잭션을 요구한다. 호출자는 조회·스캔·읽기 오류마다
// 롤백하고 커밋이 확인된 뒤에만 반환된 페이로드를 사용해야 한다.
// 커밋 결과가 불명확하면 재시도로 바꾸지 않는다.
//
// 봇별 확장(operator grant, replay/discard audit, image fallback 행, chunk 진행)은 이 패키지가
// 만든 행 id를 참조하는 봇 소유 side table이나 phase 값으로 표현하고 공용 상태 기계를 우회하지
// 않는다. reissue 세대는 client_request_id 파생(irisdurable.ReissueLadder)으로 표현하므로
// 원장 테이블을 두지 않는다.
package pgstore
