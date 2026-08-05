// Package telemetry는 OpenTelemetry tracing 설정과 context 전파 helper를
// 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 OpenTelemetry 기반 분산 추적을 서비스 공통 방식으로 구성합니다.
// Config로 service metadata, OTLP gRPC endpoint, TLS 사용 여부, sampler 비율을
// 지정하고 config.Enabled=true일 때 NewProvider가 exporter, resource, sampler,
// global provider를 설치합니다. 활성화할 때는 ServiceName과 OTLPEndpoint가
// 비어 있지 않아야 하며 SampleRate는 0과 1 사이여야 합니다. 전파기는
// W3C Trace Context(traceparent, tracestate)만 사용하고 baggage는 전파하지
// 않습니다. Enabled=false이면 no-op Provider를 반환합니다.
//
// # 주요 사용 패턴
//
//	config := telemetry.Config{}
//	config.Enabled = true
//	config.ServiceName = "llm-sched"
//	config.OTLPEndpoint = "otel-collector:4317"
//	config.SampleRate = 1.0
//	provider, err := telemetry.NewProvider(ctx, config)
//	if err != nil {
//	    return err
//	}
//	defer provider.Shutdown(ctx)
//
// # 전송 보안
//
// OTLPInsecure를 생략하면 host root CA 기반 TLS로 연결합니다. 평문 전송은
// collector가 같은 신뢰 경계 안에 있는 내부망 예외로만 사용하고, 그때만
// OTLPInsecure=true를 명시하세요. OTLPInsecure=false인 provider는
// OTEL_EXPORTER_OTLP_INSECURE, OTEL_EXPORTER_OTLP_TRACES_INSECURE 같은 환경
// 변수로 평문 강등되지 않습니다. 반대로 OTLPInsecure=false는 exporter의 TLS
// 자격증명을 host root CA로 고정하므로, OTEL_EXPORTER_OTLP_CERTIFICATE 계열
// 환경 변수로 사설 CA나 client 인증서를 주입하는 구성은 이 패키지에서 적용되지
// 않습니다.
//
// # 운영 전제
//
// span은 trace/span ID와 service metadata만 싣고 log는 사용자 식별자를 실어
// 나르므로, 두 신호를 한 저장소에서 join하면 log sink 단독으로는 없던 재식별
// 경로가 생깁니다. log sink와 OTLP collector를 같은 저장소로 합류시키지 않는
// 것을 전제로 합니다.
package telemetry
