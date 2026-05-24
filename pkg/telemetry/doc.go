// Package telemetry provides OpenTelemetry tracing setup and context
// propagation helpers.
//
// # What this package does
//
// 이 패키지는 OpenTelemetry 기반 분산 추적을 서비스 공통 방식으로 구성합니다.
// Config로 service metadata, OTLP gRPC endpoint, TLS 사용 여부, sampler 비율을
// 지정하고 config.Enabled=true일 때 NewProvider가 exporter, resource, sampler,
// global provider를 설치합니다. Enabled=false이면 no-op Provider를 반환합니다.
//
// trace context 전파는 OpenTelemetry TextMapPropagator를 통해 처리합니다.
// HTTP header, gRPC metadata, Valkey message value처럼 key/value 형태의 carrier에
// InjectContext와 ExtractContext를 적용할 수 있습니다.
//
// # 외부 surface (public API)
//
//   - Config, DefaultConfig: tracing 활성화, service metadata, OTLP endpoint, sampler 설정입니다.
//   - Provider, NewProvider: OTLP exporter와 tracer provider를 구성하는 진입점입니다.
//   - (*Provider).Shutdown: 남은 span을 flush하고 tracer provider를 종료합니다.
//   - (*Provider).IsEnabled: 실제 tracer provider 설치 여부를 반환합니다.
//   - InjectContext, ExtractContext: carrier에 trace context를 주입하거나 복원합니다.
//   - MapCarrier: map[string]string을 propagation.TextMapCarrier로 사용하는 구현입니다.
//
// # 주요 사용 패턴
//
//	config := telemetry.DefaultConfig()
//	config.Enabled = true
//	config.ServiceName = "llm-sched"
//	config.OTLPEndpoint = "otel-collector:4317"
//	config.OTLPInsecure = true
//	provider, err := telemetry.NewProvider(ctx, config)
//	if err != nil {
//	    return err
//	}
//	defer provider.Shutdown(ctx)
//
//	carrier := telemetry.MapCarrier{}
//	telemetry.InjectContext(ctx, carrier)
//	ctx = telemetry.ExtractContext(ctx, carrier)
//
// # 내부 helper 정책
//
// buildResource, buildOTLPExporterOptions, buildSampler, installGlobalProvider는
// NewProvider 내부 composition 전용입니다. 호출부는 이 helper를 복제하지 않고 Config와
// NewProvider를 통해 tracing을 구성합니다.
package telemetry
