// Package lifecycle는 서비스 기동·주기 실행·종료 정리를 제공한다.
//
// Run은 stop 신호 뒤 BeforeShutdown → runtime context 취소 → Shutdown 순서로 실행한다.
// Shutdown은 in-flight 종료를 기다리지 않고 시작하며, 별도 context와 ShutdownTimeout을 쓴다
// (0 이하면 DefaultShutdownTimeout). runtime 오류는 OnError에 관측시키고 반환값에도 보존하며,
// shutdown 오류와 함께 errors.Is/As로 구분할 수 있게 합친다. 정상 종료는 shutdown 결과만 반환한다.
//
// RunCloseSteps는 slice 순서대로 실행한다(LIFO 아님). 오류·panic·ctx 취소가 있어도 나머지를
// 실행하며, 각 step은 전달받은 ctx의 취소를 존중할지 결정한다. 오류는 "close <name>: <err>"로
// 감싸 errors.Join으로 합치며, panic 값이 error여도 errors.Is 식별성을 보존한다.
package lifecycle
