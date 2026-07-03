// Package ginauth는 gin 기반 관리 API key 인증 middleware를 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 shared-go의 프레임워크 독립 인증 비교 함수를 gin middleware로
// 감쌉니다. 빈 API key를 개발 모드로 통과시키는 일반 route middleware와,
// 인증 후에도 404를 반환하는 NoRoute handler 계약을 분리합니다.
//
// # 주요 사용 패턴
//
//	router := gin.New()
//	router.Use(ginauth.APIKeyAuthMiddleware(apiKey))
//	router.NoRoute(ginauth.NoRouteAuthHandler(apiKey))
package ginauth
