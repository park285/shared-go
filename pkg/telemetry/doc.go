// Package telemetry는 OpenTelemetry tracing 설정과 context 전파 helper를
// 제공합니다.
//
// # 패키지 개요
//
// 이 패키지는 OpenTelemetry 기반 분산 추적을 서비스 공통 방식으로 구성합니다.
// Config로 service metadata, OTLP gRPC endpoint, TLS 사용 여부, sampler 비율을
// 지정하고 config.Enabled=true일 때 NewProvider가 exporter, resource, sampler,
// global provider를 설치합니다. Enabled=false이면 no-op Provider를 반환합니다.
//
// # 외부 surface (public API)
//
//   - Config: tracing 활성화, service metadata, OTLP endpoint, sampler 설정입니다.
//   - Provider, NewProvider: OTLP exporter와 tracer provider를 구성하는 진입점입니다.
//   - (*Provider).Shutdown: 남은 span을 flush하고 tracer provider를 종료합니다.
//   - (*Provider).IsEnabled: 실제 tracer provider 설치 여부를 반환합니다.
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
//
// # 내부 helper 정책
//
// buildResource, buildOTLPExporterOptions, buildSampler, installGlobalProvider는
// NewProvider 내부 composition 전용입니다. 호출부는 이 helper를 복제하지 않고 Config와
// NewProvider를 통해 tracing을 구성합니다.
package telemetry
