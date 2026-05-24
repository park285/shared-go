// Package httpserver provides small lifecycle helpers for HTTP servers.
//
// 패키지 소개
//
// 이 패키지는 shared-go runtime 계층에서 재사용하는 HTTP server start/shutdown
// 흐름만 공통화합니다. process signal 처리, shutdown timeout 생성, runtime 별
// background worker 정리는 lifecycle 패키지와 각 runtime 이 계속 담당합니다.
//
// 외부 surface
//
// Server 는 ListenAndServe 와 Shutdown 만 요구하므로 *http.Server 가 자연스럽게
// 구현합니다. Start 는 ListenAndServe 를 background goroutine 에서 실행하고,
// http.ErrServerClosed 를 정상 종료로 취급합니다. Shutdown 은 server.Shutdown(ctx)
// 실패를 호출부가 넘긴 errorText 로 wrap 합니다.
//
// 주요 사용 패턴
//
// runtime 은 lifecycle.Run 의 Start hook 에서 Start(server, logger, errCh) 를
// 호출하고, Shutdown hook 에서 Shutdown(ctx, server, "shutdown http server") 를
// 호출합니다. server construction 과 router/H2C 조립은 호출부의 기존 책임으로
// 남깁니다.
//
// 내부 helper 정책
//
// 이 패키지는 HTTP start/shutdown error handling 만 다룹니다. signal handling,
// timeout policy, readiness 전환, scheduler stop, lease release, router 구성은
// 포함하지 않습니다.
package httpserver
