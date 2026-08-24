package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials"

	"github.com/park285/shared-go/v2/pkg/logging"
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
	// "otel-collector:4317"(host:port) 또는 "http://otel-collector:4317"(URL) 형식을 받습니다.
	// OTEL_EXPORTER_OTLP_ENDPOINT env를 그대로 넘길 때는 URL 형식이어야 otel SDK의
	// env 파싱 경고가 나지 않습니다.
	OTLPEndpoint string

	// OTLPInsecure: true면 TLS 없이 연결합니다. 내부망에서만 사용하세요.
	OTLPInsecure bool

	// SampleRate: 샘플링 비율입니다 (0.0 ~ 1.0). 1.0이면 전체 트레이싱.
	// 프로덕션에서는 0.1 ~ 0.5 권장.
	SampleRate float64
}

type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	shutdownOnce   sync.Once
	shutdownErr    error
}

// config.Enabled가 false면 no-op Provider를 반환합니다.
func NewProvider(ctx context.Context, config Config) (*Provider, error) {
	if !config.Enabled {
		return &Provider{}, nil
	}

	validatedConfig, err := validateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, buildOTLPExporterOptions(validatedConfig)...)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(buildResource(validatedConfig)),
		sdktrace.WithSampler(buildSampler(validatedConfig)),
	)
	installGlobalProvider(tp)

	return &Provider{tracerProvider: tp}, nil
}

func validateConfig(config Config) (Config, error) {
	config.ServiceName = strings.TrimSpace(config.ServiceName)
	config.OTLPEndpoint = strings.TrimSpace(config.OTLPEndpoint)

	if config.ServiceName == "" {
		return Config{}, errors.New("invalid telemetry config: ServiceName is required")
	}

	if config.OTLPEndpoint == "" {
		return Config{}, errors.New("invalid telemetry config: OTLPEndpoint is required")
	}

	if isEndpointURL(config.OTLPEndpoint) {
		parsed, err := url.Parse(config.OTLPEndpoint)
		if err != nil || parsed.Host == "" {
			return Config{}, fmt.Errorf("invalid telemetry config: OTLPEndpoint URL must include a host: %q", config.OTLPEndpoint)
		}
	}

	if math.IsNaN(config.SampleRate) || math.IsInf(config.SampleRate, 0) || config.SampleRate < 0 || config.SampleRate > 1 {
		return Config{}, errors.New("invalid telemetry config: SampleRate must be between 0 and 1")
	}

	return config, nil
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
	opts := []otlptracegrpc.Option{endpointOption(config.OTLPEndpoint)}
	if config.OTLPInsecure {
		return append(opts, otlptracegrpc.WithInsecure())
	}

	// otlptracegrpc는 OTEL_EXPORTER_OTLP_INSECURE 같은 env를 user option보다 먼저 적용한다.
	// TLS를 명시하지 않으면 env가 심은 Insecure=true가 그대로 남아 평문으로 강등되므로,
	// Insecure보다 우선 적용되는 GRPCCredentials로 TLS를 못박는다.
	return append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(nil)))
}

func isEndpointURL(endpoint string) bool {
	return strings.Contains(endpoint, "://")
}

func endpointOption(endpoint string) otlptracegrpc.Option {
	if isEndpointURL(endpoint) {
		return otlptracegrpc.WithEndpointURL(endpoint)
	}

	return otlptracegrpc.WithEndpoint(endpoint)
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
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetErrorHandler(slogErrorHandler{})
}

type slogErrorHandler struct{}

// exporter 에러 문구에는 OTLP endpoint URL이 그대로 실려 오고, 그 URL에 credential이 박혀
// 있을 수 있다. 이 핸들러는 slog.Default()로 나가므로 sanitize handler가 설치돼 있다는
// 보장이 없어, 여기서 직접 마스킹한다.
func (slogErrorHandler) Handle(err error) {
	if err == nil {
		return
	}

	slog.Warn("otel export error", "error", logging.RedactDiagnostic(err.Error()))
}

// 시그널 핸들러 defer의 이미 취소된 ctx가 와도 마지막 배치를 flush할 수 있도록
// 취소를 분리하고 5초 윈도우로 제한합니다.
const flushTimeout = 5 * time.Second

func flushContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
}

// 버퍼에 남은 span들을 flush하여 데이터 유실을 방지합니다.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tracerProvider == nil {
		return nil
	}

	p.shutdownOnce.Do(func() {
		flushCtx, cancel := flushContext(ctx)
		defer cancel()

		if err := p.tracerProvider.Shutdown(flushCtx); err != nil {
			p.shutdownErr = fmt.Errorf("shutdown otel tracer provider: %w", err)
		}
	})

	return p.shutdownErr
}

func (p *Provider) IsEnabled() bool {
	return p.tracerProvider != nil
}
