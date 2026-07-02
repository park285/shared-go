// Package retry는 pkg/backoff의 지연 값 계산을 사용해 context 취소를 존중하며
// sleep·재시도·중단을 수행하는 재시도 루프(WithRetry)를 제공합니다. backoff가 순수
// 값 helper라면 이 패키지는 루프입니다.
//
// DelayOverride가 반환한 지연도 MaxDelay로 함께 캡됩니다(API 시그니처만으로는 드러나지 않음).
package retry
