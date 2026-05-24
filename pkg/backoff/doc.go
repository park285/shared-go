// Package backoff provides shared exponential backoff helpers.
//
// 이 패키지는 호출부가 상태 기반 또는 attempt 기반 exponential backoff 값을 계산할 때
// 사용할 수 있는 작은 순수 helper를 제공합니다. 실제 sleep, retry loop, context 제어는
// 호출부 책임으로 둡니다.
package backoff
