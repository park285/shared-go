package telemetry

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.Enabled {
		t.Error("default config should be disabled")
	}
	if config.SampleRate != 1.0 {
		t.Errorf("expected sample rate 1.0, got %f", config.SampleRate)
	}
	if !config.OTLPInsecure {
		t.Error("default should be insecure")
	}
}

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

func TestBuildOTLPExporterOptions_Endpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "env-collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "env-traces:4317")

	for _, tt := range []struct {
		name     string
		endpoint string
	}{
		{name: "non-empty endpoint", endpoint: "otel-collector:4317"},
		{name: "empty endpoint", endpoint: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := buildOTLPExporterOptions(Config{
				OTLPEndpoint: tt.endpoint,
				OTLPInsecure: false,
			})

			if len(opts) != 1 {
				t.Fatalf("expected endpoint option only, got %d options", len(opts))
			}
			if got := otlpClientEndpoint(t, opts); got != tt.endpoint {
				t.Fatalf("expected client endpoint %q, got %q", tt.endpoint, got)
			}
			assertOptionConfigType(t, opts[0], "*otlpconfig.genericOption")
		})
	}
}

func TestBuildOTLPExporterOptions_InsecureTrue(t *testing.T) {
	config := Config{
		OTLPEndpoint: "otel-collector:4317",
		OTLPInsecure: true,
	}
	opts := buildOTLPExporterOptions(config)

	if len(opts) != 2 {
		t.Fatalf("expected endpoint and insecure dial options, got %d options", len(opts))
	}
	if got := otlpClientEndpoint(t, opts); got != config.OTLPEndpoint {
		t.Fatalf("expected client endpoint %q, got %q", config.OTLPEndpoint, got)
	}
	assertOptionConfigType(t, opts[0], "*otlpconfig.genericOption")
	assertOptionConfigType(t, opts[1], "*otlpconfig.grpcOption")
}

func TestBuildOTLPExporterOptions_InsecureFalse(t *testing.T) {
	config := Config{
		OTLPEndpoint: "otel-collector:4317",
		OTLPInsecure: false,
	}
	opts := buildOTLPExporterOptions(config)

	if len(opts) != 1 {
		t.Fatalf("expected insecure=false to keep only endpoint option, got %d options", len(opts))
	}
	if got := otlpClientEndpoint(t, opts); got != config.OTLPEndpoint {
		t.Fatalf("expected client endpoint %q, got %q", config.OTLPEndpoint, got)
	}
	assertOptionConfigType(t, opts[0], "*otlpconfig.genericOption")
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
	assertContainsField(t, fields, "baggage")

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
	if got := carrier.Get("baggage"); got != "tenant=test" {
		t.Fatalf("expected baggage to be injected, got %q", got)
	}
}

func TestMapCarrier_GetSetKeys(t *testing.T) {
	carrier := MapCarrier{}

	carrier.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	carrier.Set("baggage", "tenant=test")

	if got := carrier.Get("traceparent"); got != "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01" {
		t.Fatalf("unexpected traceparent value: %q", got)
	}

	keys := carrier.Keys()
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	if !keySet["traceparent"] || !keySet["baggage"] {
		t.Fatalf("expected traceparent and baggage keys, got %v", keys)
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

func TestProvider_Shutdown_Error(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	provider, err := NewProvider(context.Background(), Config{
		Enabled:      true,
		ServiceName:  "shutdown-err-test",
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		SampleRate:   1.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = provider.Shutdown(ctx)
	if err == nil {
		t.Fatal("shutdown with cancelled context should return error")
	}
	if !strings.Contains(err.Error(), "shutdown otel tracer provider") {
		t.Fatalf("expected wrapped error message, got: %v", err)
	}
}

func TestInjectContext(t *testing.T) {
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(prevProp) })

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	carrier := MapCarrier{}
	InjectContext(ctx, carrier)

	if carrier.Get("traceparent") == "" {
		t.Fatal("expected traceparent header to be injected")
	}
	if !strings.HasPrefix(carrier.Get("traceparent"), "00-") {
		t.Fatalf("unexpected traceparent format: %q", carrier.Get("traceparent"))
	}
}

func TestExtractContext(t *testing.T) {
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(prevProp) })

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	carrier := MapCarrier{
		"traceparent": "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01",
	}

	ctx := ExtractContext(context.Background(), carrier)

	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatal("expected valid span context after extraction")
	}
	wantTraceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	if sc.TraceID() != wantTraceID {
		t.Fatalf("expected trace ID %v, got %v", wantTraceID, sc.TraceID())
	}
	wantSpanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if sc.SpanID() != wantSpanID {
		t.Fatalf("expected span ID %v, got %v", wantSpanID, sc.SpanID())
	}
	if !sc.IsSampled() {
		t.Fatal("expected sampled flag to be set")
	}
}

func TestMapCarrier_GetMissingKey(t *testing.T) {
	t.Parallel()
	carrier := MapCarrier{}
	if got := carrier.Get("nonexistent"); got != "" {
		t.Fatalf("expected empty string for missing key, got %q", got)
	}
}

func TestMapCarrier_EmptyKeys(t *testing.T) {
	t.Parallel()
	carrier := MapCarrier{}
	keys := carrier.Keys()
	if len(keys) != 0 {
		t.Fatalf("expected empty keys, got %v", keys)
	}
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

func otlpClientEndpoint(t *testing.T, opts []otlptracegrpc.Option) string {
	t.Helper()

	client := otlptracegrpc.NewClient(opts...)
	value := reflect.ValueOf(client)
	if value.Kind() != reflect.Pointer {
		t.Fatalf("expected OTLP client pointer, got %T", client)
	}
	field := value.Elem().FieldByName("endpoint")
	if !field.IsValid() {
		t.Fatalf("expected OTLP client %T to expose endpoint field", client)
	}
	return field.String()
}

func assertOptionConfigType(t *testing.T, opt otlptracegrpc.Option, want string) {
	t.Helper()

	value := reflect.ValueOf(opt)
	field := value.FieldByName("GRPCOption")
	if !field.IsValid() {
		t.Fatalf("expected option %T to expose GRPCOption field", opt)
	}
	got := reflect.TypeOf(field.Interface()).String()
	if got != want {
		t.Fatalf("expected option config type %q, got %q", want, got)
	}
}

func assertContainsField(t *testing.T, fields []string, want string) {
	t.Helper()

	if slices.Contains(fields, want) {
		return
	}
	t.Fatalf("expected propagator fields to contain %q, got %v", want, fields)
}
