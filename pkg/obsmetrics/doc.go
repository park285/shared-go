// Package obsmetrics는 client_golang 없이 Prometheus 텍스트 메트릭을 생성한다.
// WebhookMetrics는 접두사별 이름을 유지하며 iris-client-go의 관측 인터페이스를
// 직접 의존성 없이 구현한다. 호출자는 iris.WithMetrics에 주입할 수 있다.
//
// 히스토그램·레이블 벡터·텍스트 출력·런타임 메트릭 도구를 제공하며 도메인 메트릭은
// 서비스가 정의한다. /metrics에서 WebhookMetrics.Expose·도메인 출력·
// WriteRuntimeMetrics를 조합한다.
package obsmetrics
