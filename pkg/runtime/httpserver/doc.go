// Package httpserver는 HTTP server를 위한 작은 lifecycle helper를 제공합니다.
//
// 패키지 소개
//
// 이 패키지는 shared-go runtime 계층에서 재사용하는 HTTP server start/shutdown
// 흐름만 공통화합니다. process signal 처리, shutdown timeout 생성, runtime 별
// background worker 정리는 lifecycle 패키지와 각 runtime 이 계속 담당합니다.
//
// 주요 사용 패턴
//
// runtime 은 lifecycle.Run 의 Start hook 에서 Start(server, logger, errCh) 를
// 호출하고, Shutdown hook 에서 Shutdown(ctx, server, "shutdown http server") 를
// 호출합니다. NewServer 는 수신 한계와 OpenTelemetry 계측을 가진 http.Server 를 만들고,
// WithBodyReadTimeout 은 handler 진입 이후의 본문 읽기 예산을 둡니다. router 조립은
// 호출부의 책임으로 남깁니다.
package httpserver
