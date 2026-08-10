package telemetry

import (
	"context"
	"errors"
	"math"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewProvider_Disabled(t *testing.T) {
	config := Config{Enabled: false}
	provider, err := NewProvider(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("provider should not be nil")
	}
	if provider.IsEnabled() {
		t.Error("disabled provider should return false for IsEnabled")
	}
}

func TestNewProvider_EnabledRejectsInvalidConfig(t *testing.T) {
	base := Config{
		Enabled:      true,
		ServiceName:  "test-service",
		OTLPEndpoint: "localhost:4317",
		SampleRate:   0.5,
	}
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{name: "missing service name", config: Config{Enabled: true, OTLPEndpoint: base.OTLPEndpoint, SampleRate: base.SampleRate}, want: "ServiceName"},
		{name: "blank service name", config: Config{Enabled: true, ServiceName: " \t\n", OTLPEndpoint: base.OTLPEndpoint, SampleRate: base.SampleRate}, want: "ServiceName"},
		{name: "missing OTLP endpoint", config: Config{Enabled: true, ServiceName: base.ServiceName, SampleRate: base.SampleRate}, want: "OTLPEndpoint"},
		{name: "blank OTLP endpoint", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: " \t\n", SampleRate: base.SampleRate}, want: "OTLPEndpoint"},
		{name: "negative sample rate", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: base.OTLPEndpoint, SampleRate: -0.01}, want: "SampleRate"},
		{name: "sample rate above one", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: base.OTLPEndpoint, SampleRate: 1.01}, want: "SampleRate"},
		{name: "not a number sample rate", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: base.OTLPEndpoint, SampleRate: math.NaN()}, want: "SampleRate"},
		{name: "positive infinity sample rate", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: base.OTLPEndpoint, SampleRate: math.Inf(1)}, want: "SampleRate"},
		{name: "negative infinity sample rate", config: Config{Enabled: true, ServiceName: base.ServiceName, OTLPEndpoint: base.OTLPEndpoint, SampleRate: math.Inf(-1)}, want: "SampleRate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(context.Background(), tt.config)
			if err == nil {
				if provider != nil {
					_ = provider.Shutdown(context.Background())
				}
				t.Fatal("expected invalid enabled config to be rejected")
			}
			if provider != nil {
				t.Fatal("invalid config must not return a provider")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error to mention %s, got %q", tt.want, err)
			}
		})
	}
}

func TestValidateConfig_TrimsRequiredValues(t *testing.T) {
	config, err := validateConfig(Config{
		Enabled:      true,
		ServiceName:  " \t test-service \n",
		OTLPEndpoint: " \t localhost:4317 \n",
		SampleRate:   0.5,
	})
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if config.ServiceName != "test-service" {
		t.Fatalf("expected trimmed service name, got %q", config.ServiceName)
	}
	if config.OTLPEndpoint != "localhost:4317" {
		t.Fatalf("expected trimmed OTLP endpoint, got %q", config.OTLPEndpoint)
	}
}

func TestProvider_Shutdown_Nil(t *testing.T) {
	provider := &Provider{}
	err := provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("shutdown on nil provider should not error: %v", err)
	}
}

func TestProvider_IsEnabled(t *testing.T) {
	t.Run("nil tracer provider", func(t *testing.T) {
		p := &Provider{tracerProvider: nil}
		if p.IsEnabled() {
			t.Error("should return false for nil tracerProvider")
		}
	})
}

func TestBuildResource_PopulatesServiceAttrs(t *testing.T) {
	config := Config{
		ServiceName:    "hololive-test",
		ServiceVersion: "1.2.3",
		Environment:    "test",
	}

	res := buildResource(config)
	if res.SchemaURL() != semconv.SchemaURL {
		t.Fatalf("expected schema URL %q, got %q", semconv.SchemaURL, res.SchemaURL())
	}

	attrs := attribute.NewSet(res.Attributes()...)
	got := make(map[attribute.Key]string)
	iter := attrs.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		got[kv.Key] = kv.Value.AsString()
	}

	assertAttributeValue(t, got, semconv.ServiceNameKey, config.ServiceName)
	assertAttributeValue(t, got, semconv.ServiceVersionKey, config.ServiceVersion)
	assertAttributeValue(t, got, semconv.DeploymentEnvironmentKey, config.Environment)
}

func TestBuildSampler_AlwaysSample(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 1.0})

	if !strings.Contains(sampler.Description(), "AlwaysOnSampler") {
		t.Fatalf("expected AlwaysOnSampler in description, got %q", sampler.Description())
	}

	for _, traceID := range []trace.TraceID{
		{0x01},
		{0x02},
		{0xff},
	} {
		result := sampler.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       traceID,
			Name:          "test",
		})
		if result.Decision != sdktrace.RecordAndSample {
			t.Fatalf("expected RecordAndSample for trace ID %v, got %v", traceID, result.Decision)
		}
	}
}

func TestBuildSampler_NeverSample(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 0})

	if !strings.Contains(sampler.Description(), "AlwaysOffSampler") {
		t.Fatalf("expected AlwaysOffSampler in description, got %q", sampler.Description())
	}

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{0x01},
		Name:          "test",
	})
	if result.Decision != sdktrace.Drop {
		t.Fatalf("expected Drop, got %v", result.Decision)
	}
}

func TestBuildSampler_TraceIDRatioBased(t *testing.T) {
	sampler := buildSampler(Config{SampleRate: 0.5})

	if !strings.Contains(sampler.Description(), "TraceIDRatioBased{0.5}") {
		t.Fatalf("expected ratio sampler in description, got %q", sampler.Description())
	}
}

func TestBuildOTLPExporterOptions_UsesConfiguredPlaintextEndpoint(t *testing.T) {
	endpoint, methods := startTraceProbe(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(ctx, buildOTLPExporterOptions(Config{
		OTLPEndpoint: endpoint,
		OTLPInsecure: true,
	})...)
	if err != nil {
		t.Fatalf("create exporter: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = exporter.Shutdown(shutdownCtx)
	})

	exportErr := exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{recordTestSpan(t)})
	if exportErr == nil {
		t.Fatal("expected the probe collector to reject the export")
	}
	select {
	case method := <-methods:
		if method != "/opentelemetry.proto.collector.trace.v1.TraceService/Export" {
			t.Fatalf("unexpected OTLP method %q", method)
		}
	case <-ctx.Done():
		t.Fatalf("configured endpoint was not reached: %v (export: %v)", ctx.Err(), exportErr)
	}
}

func TestBuildOTLPExporterOptions_DefaultsToTLS(t *testing.T) {
	endpoint, methods := startTraceProbe(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(ctx, buildOTLPExporterOptions(Config{
		OTLPEndpoint: endpoint,
		OTLPInsecure: false,
	})...)
	if err != nil {
		t.Fatalf("create exporter: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = exporter.Shutdown(shutdownCtx)
	})

	if err := exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{recordTestSpan(t)}); err == nil {
		t.Fatal("expected TLS export to a plaintext probe to fail")
	}
	select {
	case method := <-methods:
		t.Fatalf("TLS-disabled probe unexpectedly received %q", method)
	default:
	}
}

func TestBuildOTLPExporterOptions_EnvMustNotDowngradeTLS(t *testing.T) {
	for _, envKey := range []string{"OTEL_EXPORTER_OTLP_INSECURE", "OTEL_EXPORTER_OTLP_TRACES_INSECURE"} {
		t.Run(envKey, func(t *testing.T) {
			t.Setenv(envKey, "true")

			secure := exportViaProbe(t, Config{OTLPInsecure: false})
			if secure.reached {
				t.Fatalf("%s downgraded a secure exporter to plaintext: probe received %q", envKey, secure.method)
			}
			if secure.exportErr == nil {
				t.Fatal("expected TLS export to a plaintext probe to fail")
			}

			insecure := exportViaProbe(t, Config{OTLPInsecure: true})
			if !insecure.reached {
				t.Fatalf("plaintext probe was never reached, so the secure leg proves nothing (export: %v)", insecure.exportErr)
			}
			if insecure.method != "/opentelemetry.proto.collector.trace.v1.TraceService/Export" {
				t.Fatalf("unexpected OTLP method %q", insecure.method)
			}
		})
	}
}

type probeResult struct {
	exportErr error
	method    string
	reached   bool
}

func exportViaProbe(t *testing.T, config Config) probeResult {
	t.Helper()

	endpoint, methods := startTraceProbe(t)
	config.OTLPEndpoint = endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	exporter, err := otlptracegrpc.New(ctx, buildOTLPExporterOptions(config)...)
	if err != nil {
		t.Fatalf("create exporter: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = exporter.Shutdown(shutdownCtx)
	})

	result := probeResult{exportErr: exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{recordTestSpan(t)})}
	select {
	case result.method = <-methods:
		result.reached = true
	default:
	}
	return result
}

func TestInstallGlobalProvider_SetsGlobals(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	sentinelTP := sdktrace.NewTracerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = sentinelTP.Shutdown(context.Background())
	})

	installGlobalProvider(sentinelTP)

	if got := otel.GetTracerProvider(); got != sentinelTP {
		t.Fatalf("expected installed tracer provider identity %p, got %T", sentinelTP, got)
	}

	fields := otel.GetTextMapPropagator().Fields()
	assertContainsField(t, fields, "traceparent")
	assertContainsField(t, fields, "tracestate")
	if slices.Contains(fields, "baggage") {
		t.Fatalf("trace context propagator must not include baggage, got %v", fields)
	}

	carrier := propagation.MapCarrier{}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03},
		SpanID:     trace.SpanID{0x04, 0x05, 0x06},
		TraceFlags: trace.FlagsSampled,
	}))
	member, err := baggage.NewMember("tenant", "test")
	if err != nil {
		t.Fatalf("create baggage member: %v", err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatalf("create baggage: %v", err)
	}
	ctx = baggage.ContextWithBaggage(ctx, bag)

	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if carrier.Get("traceparent") == "" {
		t.Fatal("expected traceparent to be injected")
	}
	if got := carrier.Get("baggage"); got != "" {
		t.Fatalf("baggage must not be injected, got %q", got)
	}
}

func TestNewProvider_Enabled(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	config := Config{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "0.1.0",
		Environment:    "test",
		OTLPEndpoint:   "localhost:4317",
		OTLPInsecure:   true,
		SampleRate:     1.0,
	}

	provider, err := NewProvider(context.Background(), config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	if !provider.IsEnabled() {
		t.Error("enabled provider should return true for IsEnabled")
	}
	if provider.tracerProvider == nil {
		t.Fatal("tracerProvider should not be nil")
	}
}

func TestProvider_Shutdown_Success(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	provider, err := NewProvider(context.Background(), Config{
		Enabled:      true,
		ServiceName:  "shutdown-test",
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		SampleRate:   1.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should succeed: %v", err)
	}
}

func TestProvider_Shutdown_CancelledParentStillFlushes(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	prevHandler := otel.GetErrorHandler()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		otel.SetErrorHandler(prevHandler)
	})

	provider, err := NewProvider(context.Background(), Config{
		Enabled:      true,
		ServiceName:  "shutdown-cancelled-test",
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		SampleRate:   1.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := provider.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown must detach a cancelled parent and flush, got error: %v", err)
	}
}

func TestProvider_Shutdown_IdempotentConcurrent(t *testing.T) {
	shutdownErr := errors.New("exporter shutdown failed")
	exporter := &countingShutdownExporter{err: shutdownErr}
	provider := &Provider{tracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))}

	const callers = 16
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			results <- provider.Shutdown(context.Background())
		})
	}
	wg.Wait()
	close(results)

	var first error
	for err := range results {
		if first == nil {
			first = err
			continue
		}
		if err != first {
			t.Fatalf("shutdown must return the stable first result, got distinct errors %p and %p", first, err)
		}
	}
	if !errors.Is(first, shutdownErr) {
		t.Fatalf("expected wrapped exporter shutdown error, got %v", first)
	}
	if got := exporter.shutdowns.Load(); got != 1 {
		t.Fatalf("expected exporter shutdown once, got %d calls", got)
	}
	if err := provider.Shutdown(context.Background()); err != first {
		t.Fatalf("repeated shutdown must return the stable first result, got distinct error %p", err)
	}
}

type countingShutdownExporter struct {
	shutdowns atomic.Int32
	err       error
}

func (e *countingShutdownExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *countingShutdownExporter) Shutdown(context.Context) error {
	e.shutdowns.Add(1)
	return e.err
}

func TestBuildSampler_NegativeRate(t *testing.T) {
	t.Parallel()
	sampler := buildSampler(Config{SampleRate: -0.5})
	if !strings.Contains(sampler.Description(), "AlwaysOffSampler") {
		t.Fatalf("expected AlwaysOffSampler for negative rate, got %q", sampler.Description())
	}
}

func TestBuildSampler_AboveOneRate(t *testing.T) {
	t.Parallel()
	sampler := buildSampler(Config{SampleRate: 2.0})
	if !strings.Contains(sampler.Description(), "AlwaysOnSampler") {
		t.Fatalf("expected AlwaysOnSampler for rate > 1, got %q", sampler.Description())
	}
}
func assertAttributeValue(t *testing.T, attrs map[attribute.Key]string, key attribute.Key, want string) {
	t.Helper()

	if got, ok := attrs[key]; !ok || got != want {
		t.Fatalf("expected attribute %s=%q, got %q (present=%v)", key, want, got, ok)
	}
}

func startTraceProbe(t *testing.T) (string, <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for OTLP probe: %v", err)
	}
	methods := make(chan string, 1)
	server := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(stream)
		select {
		case methods <- method:
		default:
		}
		return status.Error(codes.Unimplemented, "trace probe")
	}))
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()
	return listener.Addr().String(), methods
}

func recordTestSpan(t *testing.T) sdktrace.ReadOnlySpan {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	_, span := provider.Tracer("telemetry-test").Start(context.Background(), "export-test")
	span.End()
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected one recorded span, got %d", len(ended))
	}
	return ended[0]
}

func assertContainsField(t *testing.T, fields []string, want string) {
	t.Helper()

	if slices.Contains(fields, want) {
		return
	}
	t.Fatalf("expected propagator fields to contain %q, got %v", want, fields)
}
