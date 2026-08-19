package obsmetrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func renderWebhook(t *testing.T, m *WebhookMetrics) string {
	t.Helper()

	var buf bytes.Buffer
	if !m.Expose(&buf) {
		t.Fatal("Expose returned false")
	}

	return buf.String()
}

func TestWebhookMetricsPrefixNaming(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		prefix string
		want   string
	}{
		{"chat_bot", "chat_bot_webhook_requests_total"},
		{"twentyq", "twentyq_webhook_requests_total"},
	} {
		m := NewWebhookMetrics(tc.prefix)
		m.ObserveRequest()

		body := renderWebhook(t, m)
		if !strings.Contains(body, tc.want+" 1") {
			t.Fatalf("prefix %q: body missing %q:\n%s", tc.prefix, tc.want, body)
		}
	}
}

func TestWebhookMetricsHistogramAndRetiredQueueGauge(t *testing.T) {
	t.Parallel()

	m := NewWebhookMetrics("twentyq")
	m.ObserveHandlerDuration(150 * time.Millisecond)
	m.ObserveHandlerDuration(2 * time.Second)
	m.ObserveQueueDepth(7)
	m.ObserveQueueDepth(3)

	body := renderWebhook(t, m)
	for _, want := range []string{
		"twentyq_webhook_handler_duration_seconds_count 2",
		"twentyq_webhook_handler_duration_seconds_bucket",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "twentyq_webhook_queue_depth") {
		t.Fatalf("retired scheduler metric was exported:\n%s", body)
	}
}

func TestWebhookMetricsDecodeLatencyHistogram(t *testing.T) {
	t.Parallel()

	m := NewWebhookMetrics("chat_bot")
	m.ObserveDecodeLatency(10 * time.Millisecond)
	m.ObserveDecodeLatency(250 * time.Millisecond)

	body := renderWebhook(t, m)
	for _, want := range []string{
		"chat_bot_webhook_decode_latency_seconds_count 2",
		"chat_bot_webhook_decode_latency_seconds_sum",
		"chat_bot_webhook_decode_latency_seconds_bucket",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestWebhookMetricsOmitRetiredSchedulerFamilies(t *testing.T) {
	t.Parallel()

	metrics := NewWebhookMetrics("twentyq")
	metrics.ObserveQueueDepth(7)

	body := renderWebhook(t, metrics)
	for _, forbidden := range []string{
		"twentyq_webhook_queue_depth",
		"twentyq_webhook_workers_configured",
		"twentyq_webhook_queue_size",
		"twentyq_webhook_pending",
		"twentyq_webhook_inflight",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("retired scheduler metric %q was exported", forbidden)
		}
	}
}
