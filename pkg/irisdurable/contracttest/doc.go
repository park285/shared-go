// Package contracttest는 irisdurable 계약을 구현한 저장소가 실제로 그 계약을 지키는지 검증하는
// 재사용 스위트다.
//
// 각 봇은 자기 구현을 irisdurable 인터페이스로 감싼 fixture를 Suite에 넣고 Run을 호출한다.
// Suite의 nil 항목은 건너뛰므로, 어떤 절을 통과하지 못하는 의미 차이가 있으면 그 절을 비우고
// 근거를 기록한다. 스위트는 admission 멱등성, nonce set-once, outcome_unknown 보존, bounded
// reissue, 보존·지평 계약을 검증하며 저장소 격리와 DB 제공은 fixture의 책임이다.
package contracttest
