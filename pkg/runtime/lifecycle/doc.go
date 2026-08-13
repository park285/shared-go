// Package lifecycle는 runtime 서비스의 기동·주기 실행·종료 정리를 위한 작은 helper 모음입니다.
//
// Run은 signal 구독과 shutdown timeout을 엮은 프로세스 lifecycle을, RunTickerLoop는 context
// 취소를 존중하는 ticker 루프를, CleanupCloser·Managed는 once 가드된 정리를, CloseStep·
// RunCloseSteps는 순서 있는 종료 정리를 제공합니다.
//
// # Run의 종료 순서 계약
//
// Run은 stop 신호 이후 BeforeShutdown → runtime context cancel → Shutdown 순서로 진행합니다. 즉
// Shutdown hook은 이미 취소된 runtime context 아래에서 실행되며, in-flight 작업이 스스로 멈춘 뒤가
// 아니라 취소 직후에 시작됩니다. Shutdown ctx는 별도 context이고 ShutdownTimeout(0 이하면
// DefaultShutdownTimeout)으로 bounded합니다.
//
// runtime error는 OnError에 관측용으로 전달하면서 Run 반환값에도 보존합니다. shutdown도 실패하면
// 두 error를 errors.Is/As로 각각 식별할 수 있도록 함께 반환합니다. runtime error 없이 signal이나
// BaseContext 취소로 정상 종료하면 기존과 같이 shutdown 결과만 반환합니다.
//
// # 순서 있는 종료(RunCloseSteps) 계약
//
// RunCloseSteps의 다음 계약은 시그니처만으로는 드러나지 않습니다.
//
//   - 실행 순서는 steps slice의 순서 그대로입니다. 역순(LIFO)이 아닙니다. defer 스택처럼 역순
//     정리를 원하면 호출부가 slice를 그 순서로 구성해 넘겨야 합니다.
//   - 한 step이 error를 반환해도 나머지 step을 모두 계속 실행합니다. 첫 실패에서 중단하지
//     않습니다. shutdown 자원 정리가 부분 실패로 끊기지 않도록 하기 위함입니다.
//   - ctx가 이미 취소·만료됐어도 step을 건너뛰지 않고 모두 실행합니다. 종료 정리는 취소된 ctx
//     아래에서 도는 것이 정상 경로이기 때문입니다. ctx는 각 step에 그대로 전달되며, 취소 존중
//     여부는 step 구현의 책임으로 둡니다.
//   - 각 step의 error는 "close <name>: <err>"로 wrap되어 errors.Join으로 집계됩니다. 반환값은
//     errors.Is/As로 개별 step error를 추출할 수 있는 단일 joined error이며, 전부 성공하면
//     nil입니다.
//   - step이 panic하면 recover해 error로 변환한 뒤 위 집계에 포함하고 나머지 step은 계속
//     실행합니다. panic 값이 error면 %w로 wrap해 errors.Is 식별성을 보존합니다. 한 step의
//     panic이 다른 step의 정리나 프로세스 자체를 중단시키지 않습니다.
package lifecycle
