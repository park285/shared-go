// Package obsmetrics는 client_golang 의존성 없이 Prometheus 평문 텍스트 exposition을
// 직접 생성하는 메트릭 키트입니다.
//
// # 패키지 개요
//
// 이 패키지는 iris webhook 핸들러 관측 포인트(iris-client-go webhook.Metrics)를 prefix
// 네임스페이스 단위로 구현한 WebhookMetrics를 제공합니다. prefix가 "chat_bot"이면
// chat_bot_webhook_* 계열을, "twentyq"면 twentyq_webhook_* 계열을 노출하므로, iris
// webhook을 소비하는 여러 서비스가 동일 구현을 공유하면서도 각자의 메트릭 이름을
// 보존할 수 있습니다.
//
// 도메인 메트릭은 서비스마다 다르므로 이 패키지가 직접 정의하지 않고, 같은 평문 포맷으로
// 노출할 수 있도록 Histogram 타입과 exposition 헬퍼(WriteCounter/WriteGauge/
// WriteHistogram), 그리고 런타임 메트릭 writer(WriteRuntimeMetrics)를 제공합니다.
// 각 서비스는 자신의 /metrics 핸들러에서 WebhookMetrics.WriteTo, 도메인 메트릭 직렬화,
// WriteRuntimeMetrics를 순서대로 조립합니다.
//
// WebhookMetrics는 iris-client-go를 import하지 않습니다. Go의 구조적 타이핑으로 메서드
// 시그니처만 일치시키므로, 호출측에서 iris.WithMetrics에 그대로 주입할 수 있고 이
// 패키지에는 불필요한 의존성이 생기지 않습니다.
package obsmetrics
