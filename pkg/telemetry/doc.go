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
//	config.OTLPInsecure = true
//	config.SampleRate = 1.0
//	provider, err := telemetry.NewProvider(ctx, config)
//	if err != nil {
//	    return err
//	}
//	defer provider.Shutdown(ctx)
package telemetry
