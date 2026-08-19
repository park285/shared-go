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

func TestWebhookMetricsHistogramAndGauge(t *testing.T) {
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
		"twentyq_webhook_queue_depth 3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
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

func TestWebhookDiagnosticsExported(t *testing.T) {
	t.Parallel()

	m := NewWebhookMetrics("chat_bot")
	m.SetDiagnosticsSource(func() WebhookDiagnostics {
		return WebhookDiagnostics{
			WorkersConfigured: 16,
			QueueSize:         1000,
			Pending:           4,
			InFlight:          2,
			EnqueueRejected:   9,
			QueueFullCount:    5,
			HandlerTimeouts:   3,
		}
	})

	body := renderWebhook(t, m)
	for _, want := range []string{
		"chat_bot_webhook_workers_configured 16",
		"chat_bot_webhook_queue_size 1000",
		"chat_bot_webhook_pending 4",
		"chat_bot_webhook_inflight 2",
		"chat_bot_webhook_enqueue_rejected_total 9",
		"chat_bot_webhook_queue_full_total 5",
		"chat_bot_webhook_handler_timeouts_total 3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestWebhookMetricsCanDisableLegacySchedulerFamilies(t *testing.T) {
	t.Parallel()

	metrics := NewWebhookMetrics("twentyq")
	metrics.DisableLegacyQueueMetrics()
	metrics.ObserveQueueDepth(7)
	metrics.SetDiagnosticsSource(func() WebhookDiagnostics {
		return WebhookDiagnostics{WorkersConfigured: 4, Pending: 2}
	})

	body := renderWebhook(t, metrics)
	for _, forbidden := range []string{"twentyq_webhook_queue_depth", "twentyq_webhook_workers_configured"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("legacy scheduler metric %q was exported", forbidden)
		}
	}
}
