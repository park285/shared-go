// Package ginauth는 gin 기반 관리 API key 인증 middleware를 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 shared-go의 프레임워크 독립 인증 비교 함수를 gin middleware로
// 감쌉니다. 빈 API key는 기본적으로 503으로 fail-closed 처리하며, 인증을 끄려면
// AuthConfig.Disabled를 명시해야 합니다.
//
// # 주요 사용 패턴
//
//	router := gin.New()
//	cfg := ginauth.AuthConfig{APIKey: apiKey}
//	router.Use(ginauth.AuthMiddleware(cfg))
//	router.NoRoute(ginauth.NoRouteHandler(cfg))
package ginauth
