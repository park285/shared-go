package workerconfig

import (
	"strings"
	"testing"
	"time"
)

func TestSG06WorkerProfileRejectsOversizeCapacity_c24c67e0(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(p *IrisBotWebhookWorkerProfile)
		want   string
	}{
		{
			name:   "lane_workers",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Delivery.LaneWorkers = 2_000_000_000 },
			want:   "delivery.lane_workers must be <=",
		},
		{
			name:   "lane_queue_capacity",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Delivery.LaneQueueCapacity = 2_000_000_000 },
			want:   "delivery.lane_queue_capacity must be <=",
		},
		{
			name:   "max_global_in_flight",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Delivery.MaxGlobalInFlight = 2_000_000_000 },
			want:   "delivery.max_global_in_flight must be <=",
		},
		{
			name:   "receive_workers",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Receive.Workers = 2_000_000_000 },
			want:   "receive.workers must be <=",
		},
		{
			name:   "receive_queue_size",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Receive.QueueSize = 2_000_000_000 },
			want:   "receive.queue_size must be <=",
		},
		{
			name:   "bot_pool_workers",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.BotPool.Workers = 2_000_000_000 },
			want:   "bot_pool.workers must be <=",
		},
		{
			name:   "request_timeout",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Delivery.RequestTimeout = 24 * time.Hour },
			want:   "delivery.request_timeout_ms must be <=",
		},
		{
			name:   "receive_max_body_bytes",
			mutate: func(p *IrisBotWebhookWorkerProfile) { p.Receive.MaxBodyBytes = 1 << 40 },
			want:   "receive.max_body_bytes must be <=",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := defaultIrisBotWebhookWorkerProfile()
			tc.mutate(&profile)

			err := profile.Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want oversize rejection for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestSG06WorkerProfileAcceptsRealisticCapacity_c24c67e0(t *testing.T) {
	t.Parallel()

	if err := defaultIrisBotWebhookWorkerProfile().Validate(); err != nil {
		t.Fatalf("Validate(default) error = %v, want nil", err)
	}

	scaled := defaultIrisBotWebhookWorkerProfile()
	scaled.Delivery.LaneWorkers = 256
	scaled.Delivery.LaneQueueCapacity = 8192
	scaled.Delivery.MaxGlobalInFlight = 1024
	scaled.Delivery.MaxPerEndpointInFlight = 128
	scaled.Delivery.MaxDrainPerTick = 4096
	scaled.Receive.Workers = 512
	scaled.Receive.QueueSize = 16384
	scaled.BotPool.Workers = 128
	scaled.BotPool.QueueSize = 1024

	if err := scaled.Validate(); err != nil {
		t.Fatalf("Validate(scaled realistic) error = %v, want nil", err)
	}
}
