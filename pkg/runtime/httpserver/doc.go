// Package httpserver는 HTTP 서버 생성·기동·종료를 공통화한다.
// 신호 처리·종료 기한·백그라운드 작업 정리는 lifecycle과 호출자가 담당한다.
//
// lifecycle.Run의 Start·Shutdown 훅에서 각각 Start·Shutdown을 호출한다.
// NewServer는 수신 한계·OpenTelemetry 계측을, WithBodyReadTimeout은
// 핸들러 진입 이후 본문 읽기 기한을 적용한다. 라우터 구성은 호출자 책임이다.
package httpserver
