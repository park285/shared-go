// Package httpserver는 HTTP 서버 생성·기동·종료를 공통화한다.
// 신호 처리·종료 기한·백그라운드 작업 정리는 lifecycle과 호출자가 담당한다.
//
// 서버 실행 정본은 두 가지다. 종료 ctx로 수명을 닫는 프로세스는 Run(ctx, server, timeout)을 쓰고,
// lifecycle.Run 훅 안에서는 Start 훅에서 StartServerWithPrefix를, Shutdown 훅에서 Shutdown을 호출한다.
// NewServer는 수신 한계·OpenTelemetry 계측을, WithBodyReadTimeout은
// 핸들러 진입 이후 본문 읽기 기한을 적용한다. 라우터 구성은 호출자 책임이다.
package httpserver
