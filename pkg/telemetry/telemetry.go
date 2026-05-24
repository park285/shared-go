package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	// Enabled: true면 트레이싱을 활성화합니다.
	Enabled bool

	// ServiceName: 서비스 식별자입니다 (예: "mcp-llm-server").
	ServiceName string

	// ServiceVersion: 서비스 버전입니다 (예: "1.0.0").
	ServiceVersion string

	// Environment: 배포 환경입니다 (예: "production", "development").
	Environment string

	// OTLPEndpoint: OTLP collector/exporter 주소입니다.
	// 예: "otel-collector:4317" (gRPC) 또는 "otel-collector:4318" (HTTP)
	OTLPEndpoint string

	// OTLPInsecure: true면 TLS 없이 연결합니다. 내부망에서만 사용하세요.
	OTLPInsecure bool

	// SampleRate: 샘플링 비율입니다 (0.0 ~ 1.0). 1.0이면 전체 트레이싱.
	// 프로덕션에서는 0.1 ~ 0.5 권장.
	SampleRate float64
}

func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		SampleRate:   1.0,
		OTLPInsecure: true,
	}
}

type Provider struct {
	tracerProvider *sdktrace.TracerProvider
}

// config.Enabled가 false면 no-op Provider를 반환합니다.
func NewProvider(ctx context.Context, config Config) (*Provider, error) {
	if !config.Enabled {
		return &Provider{}, nil
	}

	exporter, err := otlptracegrpc.New(ctx, buildOTLPExporterOptions(config)...)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(buildResource(config)),
		sdktrace.WithSampler(buildSampler(config)),
	)
	installGlobalProvider(tp)

	return &Provider{tracerProvider: tp}, nil
}

func buildResource(config Config) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion(config.ServiceVersion),
		semconv.DeploymentEnvironment(config.Environment),
	)
}

func buildOTLPExporterOptions(config Config) []otlptracegrpc.Option {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
	}
	if config.OTLPInsecure {
		opts = append(opts,
			otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	}
	return opts
}

func buildSampler(config Config) sdktrace.Sampler {
	var rootSampler sdktrace.Sampler
	if config.SampleRate >= 1.0 {
		rootSampler = sdktrace.AlwaysSample()
	} else if config.SampleRate <= 0 {
		rootSampler = sdktrace.NeverSample()
	} else {
		rootSampler = sdktrace.TraceIDRatioBased(config.SampleRate)
	}
	return sdktrace.ParentBased(rootSampler)
}

func installGlobalProvider(tp *sdktrace.TracerProvider) {
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)
}

// 버퍼에 남은 span들을 flush하여 데이터 유실을 방지합니다.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tracerProvider == nil {
		return nil
	}
	if err := p.tracerProvider.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown otel tracer provider: %w", err)
	}
	return nil
}

func (p *Provider) IsEnabled() bool {
	return p.tracerProvider != nil
}

// gRPC metadata나 HTTP headers로 trace context를 전파할 때 사용합니다.
func InjectContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// 메시지 헤더나 HTTP headers에서 부모 trace context를 복원할 때 사용합니다.
func ExtractContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// Valkey 메시지의 Values 필드를 직접 사용할 수 있습니다.
type MapCarrier map[string]string

func (c MapCarrier) Get(key string) string {
	return c[key]
}

func (c MapCarrier) Set(key, value string) {
	c[key] = value
}

func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
