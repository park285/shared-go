// Package retry는 pkg/backoff의 지연 값 계산을 사용해 context 취소를 존중하며
// sleep·재시도·중단을 수행하는 재시도 루프(WithRetry)를 제공합니다. backoff가 순수
// 값 helper라면 이 패키지는 루프입니다.
//
// DelayOverride가 반환한 지연도 MaxDelay로 함께 캡됩니다(API 시그니처만으로는 드러나지 않음).
//
// context 취소·만료와 직전 fn 에러가 공존하면 두 원인을 errors.Join(마지막 fn 에러, ctx.Err())로
// 함께 반환합니다. 한 번이라도 fn이 실패한 뒤 ctx가 취소·만료되면 — 다음 시도 진입 시점의 ctx
// 검사에서든 sleep이 취소로 중단됐든 — errors.Is로 마지막 fn 에러와 context.Canceled 또는
// context.DeadlineExceeded를 모두 판별할 수 있습니다. ctx가 취소됐고 직전 fn 에러가 없을
// 때(예: 첫 시도 전 취소)에는 "context error: <ctx.Err()>"로 wrap된 context 에러를 반환합니다.
// ctx가 살아 있는 상태에서 주입된 Sleep이 false를 반환하면 마지막 fn 에러만 반환합니다.
package retry
