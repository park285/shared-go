package workerconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func decodeWorkerProfileForTest(t *testing.T, workerProfileJSON string) (IrisBotWebhookWorkerProfile, error) {
	t.Helper()
	return decodeRuntimeWorkerProfile(json.RawMessage(workerProfileJSON))
}

func TestDefaultIrisBotWebhookWorkerProfilePreservesCurrentCapacity(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()

	if profile.Version != CurrentVersion {
		t.Fatalf("Version = %d, want %d", profile.Version, CurrentVersion)
	}
	if profile.ProfileID != defaultProfileID {
		t.Fatalf("ProfileID = %q, want %q", profile.ProfileID, defaultProfileID)
	}
	if profile.Delivery.LaneWorkers != 32 {
		t.Fatalf("Delivery.LaneWorkers = %d, want 32", profile.Delivery.LaneWorkers)
	}
	if profile.Delivery.LaneQueueCapacity != 128 {
		t.Fatalf("Delivery.LaneQueueCapacity = %d, want 128", profile.Delivery.LaneQueueCapacity)
	}
	if profile.Delivery.MaxGlobalInFlight != 32 {
		t.Fatalf("Delivery.MaxGlobalInFlight = %d, want 32", profile.Delivery.MaxGlobalInFlight)
	}
	if profile.Delivery.MaxPerEndpointInFlight != 8 {
		t.Fatalf("Delivery.MaxPerEndpointInFlight = %d, want 8", profile.Delivery.MaxPerEndpointInFlight)
	}
	if profile.Delivery.MaxAttempts != 6 {
		t.Fatalf("Delivery.MaxAttempts = %d, want 6", profile.Delivery.MaxAttempts)
	}
	if profile.Receive.Workers != 16 {
		t.Fatalf("Receive.Workers = %d, want 16", profile.Receive.Workers)
	}
	if profile.Receive.QueueSize != 1000 {
		t.Fatalf("Receive.QueueSize = %d, want 1000", profile.Receive.QueueSize)
	}
	if profile.Receive.MaxBodyBytes != 64<<10 {
		t.Fatalf("Receive.MaxBodyBytes = %d, want 65536", profile.Receive.MaxBodyBytes)
	}
	if profile.Receive.DedupTTL != 16*time.Minute {
		t.Fatalf("Receive.DedupTTL = %v, want 16m", profile.Receive.DedupTTL)
	}
	if profile.BotPool.Workers != 10 {
		t.Fatalf("BotPool.Workers = %d, want 10", profile.BotPool.Workers)
	}
	if profile.BotPool.QueueSize != 100 {
		t.Fatalf("BotPool.QueueSize = %d, want 100", profile.BotPool.QueueSize)
	}
	if len(profile.ProfileHash()) != 64 {
		t.Fatalf("ProfileHash() length = %d, want 64", len(profile.ProfileHash()))
	}
}

func TestRuntimeDiagnosticsDecodesFieldConversions(t *testing.T) {
	profile, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "test-profile",
		"delivery": {
			"lane_workers": 24,
			"lane_queue_capacity": 96,
			"max_global_in_flight": 24,
			"max_per_endpoint_in_flight": 6,
			"max_drain_per_tick": 80,
			"max_attempts": 5,
			"request_timeout_ms": 25000,
			"lane_idle_timeout_ms": 500
		},
		"receive": {
			"workers": 12,
			"queue_size": 128,
			"enqueue_timeout_ms": 75,
			"handler_timeout_ms": 30000,
			"max_body_bytes": 131072,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 250
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v", err)
	}

	if profile.ProfileID != "test-profile" {
		t.Fatalf("ProfileID = %q, want test-profile", profile.ProfileID)
	}
	if profile.Delivery.RequestTimeout != 25*time.Second {
		t.Fatalf("Delivery.RequestTimeout = %v, want 25s", profile.Delivery.RequestTimeout)
	}
	if profile.Receive.EnqueueTimeout != 75*time.Millisecond {
		t.Fatalf("Receive.EnqueueTimeout = %v, want 75ms", profile.Receive.EnqueueTimeout)
	}
	if profile.Receive.DedupTTL != 16*time.Minute {
		t.Fatalf("Receive.DedupTTL = %v, want 16m", profile.Receive.DedupTTL)
	}
	if !strings.Contains(string(profile.CanonicalJSON()), `"lane_queue_capacity":96`) {
		t.Fatalf("CanonicalJSON() = %s, want snake_case delivery fields", profile.CanonicalJSON())
	}
	if len(profile.ProfileHash()) != 64 {
		t.Fatalf("ProfileHash() length = %d, want 64", len(profile.ProfileHash()))
	}
}

func TestRuntimeDiagnosticsDefaultsBreakerFieldsIntoCanonicalProfile(t *testing.T) {
	profile, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "legacy-v1-profile",
		"delivery": {
			"lane_workers": 32,
			"lane_queue_capacity": 128,
			"max_global_in_flight": 32,
			"max_per_endpoint_in_flight": 8,
			"max_drain_per_tick": 128,
			"max_attempts": 6,
			"request_timeout_ms": 125000,
			"lane_idle_timeout_ms": 750
		},
		"receive": {
			"workers": 16,
			"queue_size": 1000,
			"enqueue_timeout_ms": 50,
			"handler_timeout_ms": 120000,
			"max_body_bytes": 65536,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 200
		},
		"bot_pool": {
			"workers": 10,
			"queue_size": 100
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v", err)
	}

	want := `"breaker_failure_threshold":5,"breaker_cooldown_ms":30000`
	if got := string(profile.CanonicalJSON()); !strings.Contains(got, want) {
		t.Fatalf("CanonicalJSON() = %s, want %s", got, want)
	}
}

func TestRuntimeDiagnosticsRejectsNullBreakerFields(t *testing.T) {
	_, err := decodeWorkerProfileForTest(t, `{
		"version":1,"profile_id":"null-breaker",
		"delivery":{"lane_workers":32,"lane_queue_capacity":128,"max_global_in_flight":32,"max_per_endpoint_in_flight":8,"max_drain_per_tick":128,"max_attempts":6,"request_timeout_ms":30000,"lane_idle_timeout_ms":750,"breaker_failure_threshold":null,"breaker_cooldown_ms":30000},
		"receive":{"workers":16,"queue_size":1000,"enqueue_timeout_ms":50,"handler_timeout_ms":30000,"max_body_bytes":65536,"dedup_ttl_ms":60000,"dedup_timeout_ms":200},
		"bot_pool":{"workers":10,"queue_size":100},
		"validation":{"min_queue_per_endpoint_multiplier":4,"require_receive_capacity_for_endpoint_burst":true}
	}`)
	if err == nil || !strings.Contains(err.Error(), "must not be null") {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want null rejection", err)
	}
}

func TestCanonicalWorkerProfileV1Golden(t *testing.T) {
	raw, err := os.ReadFile("testdata/worker-profile-v1-legacy.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	profile, err := decodeWorkerProfileForTest(t, string(raw))
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v", err)
	}

	const wantHash = "b69a100f0ac023758ab84c82fa8b21596d59f6c1f81e6c05ab93f2a08336f403"
	if got := profile.ProfileHash(); got != wantHash {
		t.Fatalf("ProfileHash() = %s, want %s; canonical=%s", got, wantHash, profile.CanonicalJSON())
	}
}

func TestRuntimeDiagnosticsDecodesExplicitBotPool(t *testing.T) {
	profile, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "botpool-test",
		"delivery": {
			"lane_workers": 32,
			"lane_queue_capacity": 128,
			"max_global_in_flight": 32,
			"max_per_endpoint_in_flight": 8,
			"max_drain_per_tick": 128,
			"max_attempts": 6,
			"request_timeout_ms": 125000,
			"lane_idle_timeout_ms": 750
		},
		"receive": {
			"workers": 16,
			"queue_size": 1000,
			"enqueue_timeout_ms": 50,
			"handler_timeout_ms": 120000,
			"max_body_bytes": 65536,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 200
		},
		"bot_pool": {
			"workers": 20,
			"queue_size": 200
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v", err)
	}
	if profile.BotPool.Workers != 20 {
		t.Fatalf("BotPool.Workers = %d, want 20", profile.BotPool.Workers)
	}
	if profile.BotPool.QueueSize != 200 {
		t.Fatalf("BotPool.QueueSize = %d, want 200", profile.BotPool.QueueSize)
	}
	if !strings.Contains(string(profile.CanonicalJSON()), `"bot_pool"`) {
		t.Fatalf("CanonicalJSON() missing bot_pool section")
	}
}

func TestRuntimeDiagnosticsFallsBackToDefaultBotPoolWhenMissing(t *testing.T) {
	profile, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "no-botpool",
		"delivery": {
			"lane_workers": 32,
			"lane_queue_capacity": 128,
			"max_global_in_flight": 32,
			"max_per_endpoint_in_flight": 8,
			"max_drain_per_tick": 128,
			"max_attempts": 6,
			"request_timeout_ms": 125000,
			"lane_idle_timeout_ms": 750
		},
		"receive": {
			"workers": 16,
			"queue_size": 1000,
			"enqueue_timeout_ms": 50,
			"handler_timeout_ms": 120000,
			"max_body_bytes": 65536,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 200
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v", err)
	}
	if profile.BotPool.Workers != 10 {
		t.Fatalf("BotPool.Workers = %d, want 10 (default fallback)", profile.BotPool.Workers)
	}
	if profile.BotPool.QueueSize != 100 {
		t.Fatalf("BotPool.QueueSize = %d, want 100 (default fallback)", profile.BotPool.QueueSize)
	}
}

func TestRuntimeDiagnosticsRejectsExplicitZeroBotPool(t *testing.T) {
	_, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "zero-botpool",
		"delivery": {
			"lane_workers": 32,
			"lane_queue_capacity": 128,
			"max_global_in_flight": 32,
			"max_per_endpoint_in_flight": 8,
			"max_drain_per_tick": 128,
			"max_attempts": 6,
			"request_timeout_ms": 125000,
			"lane_idle_timeout_ms": 750
		},
		"receive": {
			"workers": 16,
			"queue_size": 1000,
			"enqueue_timeout_ms": 50,
			"handler_timeout_ms": 120000,
			"max_body_bytes": 65536,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 200
		},
		"bot_pool": {
			"workers": 0,
			"queue_size": 0
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err == nil {
		t.Fatal("decodeRuntimeWorkerProfile() error = nil, want bot_pool validation error")
	}
	if !strings.Contains(err.Error(), "bot_pool.workers") {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want bot_pool.workers error", err)
	}
	if !strings.Contains(err.Error(), "bot_pool.queue_size") {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want bot_pool.queue_size error", err)
	}
}

func TestRuntimeDiagnosticsRejectsNullBotPool(t *testing.T) {
	_, err := decodeWorkerProfileForTest(t, `{
		"version": 1,
		"profile_id": "null-botpool",
		"delivery": {
			"lane_workers": 32,
			"lane_queue_capacity": 128,
			"max_global_in_flight": 32,
			"max_per_endpoint_in_flight": 8,
			"max_drain_per_tick": 128,
			"max_attempts": 6,
			"request_timeout_ms": 125000,
			"lane_idle_timeout_ms": 750
		},
		"receive": {
			"workers": 16,
			"queue_size": 1000,
			"enqueue_timeout_ms": 50,
			"handler_timeout_ms": 120000,
			"max_body_bytes": 65536,
			"dedup_ttl_ms": 960000,
			"dedup_timeout_ms": 200
		},
		"bot_pool": null,
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`)
	if err == nil {
		t.Fatal("decodeRuntimeWorkerProfile() error = nil, want bot_pool validation error")
	}
	if !strings.Contains(err.Error(), "bot_pool.workers") {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want bot_pool.workers error", err)
	}
	if !strings.Contains(err.Error(), "bot_pool.queue_size") {
		t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want bot_pool.queue_size error", err)
	}
}

func TestValidateRejectsBotPoolZeroWorkers(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.BotPool.Workers = 0

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want bot_pool.workers error")
	}
	if !strings.Contains(err.Error(), "bot_pool.workers") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsBotPoolZeroQueueSize(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.BotPool.QueueSize = 0

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want bot_pool.queue_size error")
	}
	if !strings.Contains(err.Error(), "bot_pool.queue_size") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsReceiveCapacityBelowDeliveryBurst(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.Delivery.MaxPerEndpointInFlight = 16
	profile.Receive.Workers = 4
	profile.Receive.QueueSize = 32

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want receive queue capacity error")
	}
	if !strings.Contains(err.Error(), "receive.queue_size must be >= delivery.max_per_endpoint_in_flight") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsDeliveryTimeoutBeyondReceiveHandlerOverhang(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.Delivery.RequestTimeout = 40 * time.Second
	profile.Receive.HandlerTimeout = 30 * time.Second

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want timeout overhang rejection")
	}
	const want = "delivery.request_timeout_ms must be <= receive.handler_timeout_ms + 5000"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() error = %q, want to contain %q", err, want)
	}
}

func TestValidateRejectsEnabledBreakerWithoutCooldown(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.Delivery.BreakerFailureThreshold = 5
	profile.Delivery.BreakerCooldown = 0

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want breaker cooldown error")
	}
	if !strings.Contains(err.Error(), "delivery.breaker_cooldown_ms must be > 0 when breaker_failure_threshold > 0") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRequiresDedupTTLToOutliveDeliveryHorizon(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	if got := maxDeliveryHorizon(profile.Delivery); got != 15*time.Minute {
		t.Fatalf("maxDeliveryHorizon() = %v, want 15m", got)
	}

	profile.Receive.DedupTTL = maxDeliveryHorizon(profile.Delivery)
	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want delivery horizon rejection")
	}
	if !strings.Contains(err.Error(), "receive.dedup_ttl_ms must be greater than the maximum delivery horizon") {
		t.Fatalf("Validate() error = %v", err)
	}

	profile.Receive.DedupTTL += time.Nanosecond
	if err := profile.Validate(); err == nil {
		t.Fatal("Validate() error = nil for a duration that truncates to the delivery horizon on the millisecond wire")
	}

	profile.Receive.DedupTTL = maxDeliveryHorizon(profile.Delivery) + time.Millisecond
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error above horizon = %v", err)
	}

	roundTripped, err := decodeWorkerProfileForTest(t, string(profile.CanonicalJSON()))
	if err != nil {
		t.Fatalf("decodeRuntimeWorkerProfile(CanonicalJSON()) error = %v", err)
	}
	if roundTripped.Receive.DedupTTL != profile.Receive.DedupTTL {
		t.Fatalf("round-trip DedupTTL = %v, want %v", roundTripped.Receive.DedupTTL, profile.Receive.DedupTTL)
	}
}

func TestValidateBoundsBreakerCooldownBeforeDeliveryHorizonArithmetic(t *testing.T) {
	profile := defaultIrisBotWebhookWorkerProfile()
	profile.Delivery.MaxAttempts = maxAttempts
	profile.Delivery.BreakerCooldown = 3 * 365 * 24 * time.Hour
	profile.Receive.DedupTTL = maxDuration

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want breaker cooldown bound rejection")
	}
	if !strings.Contains(err.Error(), "delivery.breaker_cooldown_ms must be <= 1h0m0s") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRuntimeDiagnosticsRejectsRawDurationMillisecondsOverflow(t *testing.T) {
	base, err := os.ReadFile("testdata/worker-profile-v1-legacy.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	const overflowMilliseconds = (1 << 58) + 1000
	overflow := fmt.Sprintf("%d", overflowMilliseconds)
	cases := []struct {
		name   string
		field  string
		mutate func(string) string
	}{
		{name: "request timeout", field: "delivery.request_timeout_ms", mutate: replaceWorkerProfileField(`"request_timeout_ms": 125000`, `"request_timeout_ms": `+overflow)},
		{name: "lane idle timeout", field: "delivery.lane_idle_timeout_ms", mutate: replaceWorkerProfileField(`"lane_idle_timeout_ms": 750`, `"lane_idle_timeout_ms": `+overflow)},
		{name: "breaker cooldown", field: "delivery.breaker_cooldown_ms", mutate: replaceWorkerProfileField(`"lane_idle_timeout_ms": 750`, `"lane_idle_timeout_ms": 750, "breaker_failure_threshold": 5, "breaker_cooldown_ms": `+overflow)},
		{name: "enqueue timeout", field: "receive.enqueue_timeout_ms", mutate: replaceWorkerProfileField(`"enqueue_timeout_ms": 50`, `"enqueue_timeout_ms": `+overflow)},
		{name: "handler timeout", field: "receive.handler_timeout_ms", mutate: replaceWorkerProfileField(`"handler_timeout_ms": 120000`, `"handler_timeout_ms": `+overflow)},
		{name: "dedup ttl", field: "receive.dedup_ttl_ms", mutate: replaceWorkerProfileField(`"dedup_ttl_ms": 960000`, `"dedup_ttl_ms": `+overflow)},
		{name: "dedup timeout", field: "receive.dedup_timeout_ms", mutate: replaceWorkerProfileField(`"dedup_timeout_ms": 200`, `"dedup_timeout_ms": `+overflow)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := decodeWorkerProfileForTest(t, testCase.mutate(string(base)))
			if err == nil {
				t.Fatal("decodeRuntimeWorkerProfile() error = nil, want raw millisecond overflow rejection")
			}
			want := testCase.field + " must be <= 1h0m0s"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want %q", err, want)
			}
		})
	}
}

func TestRuntimeDiagnosticsAggregatesRawDurationOverflow(t *testing.T) {
	base, err := os.ReadFile("testdata/worker-profile-v1-legacy.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	const overflow = `288230376151712744`
	payload := strings.Replace(string(base), `"request_timeout_ms": 125000`, `"request_timeout_ms": `+overflow, 1)
	payload = strings.Replace(payload, `"dedup_timeout_ms": 200`, `"dedup_timeout_ms": `+overflow, 1)

	_, err = decodeWorkerProfileForTest(t, payload)
	if err == nil {
		t.Fatal("decodeRuntimeWorkerProfile() error = nil, want aggregated overflow rejection")
	}
	for _, field := range []string{"delivery.request_timeout_ms", "receive.dedup_timeout_ms"} {
		if !strings.Contains(err.Error(), field+" must be <= 1h0m0s") {
			t.Fatalf("decodeRuntimeWorkerProfile() error = %v, want aggregated %s error", err, field)
		}
	}
}

func replaceWorkerProfileField(old, replacement string) func(string) string {
	return func(payload string) string {
		return strings.Replace(payload, old, replacement, 1)
	}
}
