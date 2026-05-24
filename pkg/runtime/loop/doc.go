// Package loop provides reusable runtime loop helpers.
//
// 이 패키지는 서비스 런타임에서 반복되는 ticker 기반 loop 흐름을 공통화합니다.
// 호출부는 최초 즉시 실행 여부를 직접 결정하고, 이 패키지는 주기 실행, context 종료,
// tick error 전파만 담당합니다.
package loop
