package workerconfig

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultIrisBotWebhookWorkerProfilePreservesCurrentCapacity(t *testing.T) {
	profile := DefaultIrisBotWebhookWorkerProfile()

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

func TestDecodeIrisBotWebhookWorkerProfile(t *testing.T) {
	profile, err := DecodeIrisBotWebhookWorkerProfile(strings.NewReader(`{
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
			"dedup_ttl_ms": 90000,
			"dedup_timeout_ms": 250
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfile() error = %v", err)
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
	if profile.Receive.DedupTTL != 90*time.Second {
		t.Fatalf("Receive.DedupTTL = %v, want 90s", profile.Receive.DedupTTL)
	}
	if !strings.Contains(string(profile.CanonicalJSON()), `"lane_queue_capacity":96`) {
		t.Fatalf("CanonicalJSON() = %s, want snake_case delivery fields", profile.CanonicalJSON())
	}
	if len(profile.ProfileHash()) != 64 {
		t.Fatalf("ProfileHash() length = %d, want 64", len(profile.ProfileHash()))
	}
}

func TestDecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(t *testing.T) {
	profile, err := DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(strings.NewReader(`{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": true,
					"workerProfile": {
						"version": 1,
						"profile_id": "api-profile",
						"delivery": {
							"lane_workers": 32,
							"lane_queue_capacity": 128,
							"max_global_in_flight": 32,
							"max_per_endpoint_in_flight": 8,
							"max_drain_per_tick": 128,
							"max_attempts": 6,
							"request_timeout_ms": 30000,
							"lane_idle_timeout_ms": 750
						},
						"receive": {
							"workers": 20,
							"queue_size": 640,
							"enqueue_timeout_ms": 80,
							"handler_timeout_ms": 36000,
							"max_body_bytes": 262144,
							"dedup_ttl_ms": 120000,
							"dedup_timeout_ms": 300
						},
						"validation": {
							"min_queue_per_endpoint_multiplier": 4,
							"require_receive_capacity_for_endpoint_burst": true
						}
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics() error = %v", err)
	}
	if profile.ProfileID != "api-profile" {
		t.Fatalf("ProfileID = %q, want api-profile", profile.ProfileID)
	}
	if profile.Receive.Workers != 20 || profile.Receive.QueueSize != 640 {
		t.Fatalf("Receive = (%d,%d), want (20,640)", profile.Receive.Workers, profile.Receive.QueueSize)
	}
}

func TestDecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnosticsFallsBackToLegacyWhenDisabled(t *testing.T) {
	profile, err := DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(strings.NewReader(`{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {
					"profileEnabled": false
				}
			}
		}
	}`))
	if !errors.Is(err, ErrWorkerProfileDisabled) {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics() error = %v, want ErrWorkerProfileDisabled", err)
	}
	if profile.ProfileID != "legacy-hardcoded" {
		t.Fatalf("ProfileID = %q, want legacy-hardcoded", profile.ProfileID)
	}
}

func TestDecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnosticsRejectsMissingProfileEnabled(t *testing.T) {
	_, err := DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(strings.NewReader(`{
		"state": "running",
		"workers": {
			"webhook": {
				"webhookPipeline": {}
			}
		}
	}`))
	if err == nil {
		t.Fatal("DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics() error = nil, want missing profileEnabled error")
	}
	if !strings.Contains(err.Error(), "profileEnabled is missing") {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics() error = %v", err)
	}
}

func TestDecodeIrisBotWebhookWorkerProfileWithBotPool(t *testing.T) {
	profile, err := DecodeIrisBotWebhookWorkerProfile(strings.NewReader(`{
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
			"dedup_ttl_ms": 60000,
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
	}`))
	if err != nil {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfile() error = %v", err)
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

func TestFromWireFallsBackToDefaultBotPoolWhenMissing(t *testing.T) {
	profile, err := DecodeIrisBotWebhookWorkerProfile(strings.NewReader(`{
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
			"dedup_ttl_ms": 60000,
			"dedup_timeout_ms": 200
		},
		"validation": {
			"min_queue_per_endpoint_multiplier": 4,
			"require_receive_capacity_for_endpoint_burst": true
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeIrisBotWebhookWorkerProfile() error = %v", err)
	}
	if profile.BotPool.Workers != 10 {
		t.Fatalf("BotPool.Workers = %d, want 10 (default fallback)", profile.BotPool.Workers)
	}
	if profile.BotPool.QueueSize != 100 {
		t.Fatalf("BotPool.QueueSize = %d, want 100 (default fallback)", profile.BotPool.QueueSize)
	}
}

func TestValidateRejectsBotPoolZeroWorkers(t *testing.T) {
	profile := DefaultIrisBotWebhookWorkerProfile()
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
	profile := DefaultIrisBotWebhookWorkerProfile()
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
	profile := DefaultIrisBotWebhookWorkerProfile()
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

func TestValidateRejectsDeliveryTimeoutBeyondReceiveHandlerBudget(t *testing.T) {
	profile := DefaultIrisBotWebhookWorkerProfile()
	profile.Delivery.RequestTimeout = 40 * time.Second
	profile.Receive.HandlerTimeout = 30 * time.Second

	err := profile.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want timeout budget error")
	}
	if !strings.Contains(err.Error(), "delivery.request_timeout_ms must fit receive.handler_timeout_ms") {
		t.Fatalf("Validate() error = %v", err)
	}
}
