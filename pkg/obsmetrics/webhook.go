package obsmetrics

import (
	"io"
	"sync/atomic"
	"time"
)

var WebhookLatencyBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
}

type WebhookMetrics struct {
	prefix string

	requests        atomic.Uint64
	unauthorized    atomic.Uint64
	badRequest      atomic.Uint64
	duplicates      atomic.Uint64
	enqueueFailures atomic.Uint64
	accepted        atomic.Uint64

	handlerDuration *Histogram
	decodeLatency   *Histogram
}

func NewWebhookMetrics(prefix string) *WebhookMetrics {
	return &WebhookMetrics{
		prefix:          prefix,
		handlerDuration: NewHistogram(WebhookLatencyBuckets),
		decodeLatency:   NewHistogram(WebhookLatencyBuckets),
	}
}

func (m *WebhookMetrics) ObserveRequest() {
	if m != nil {
		m.requests.Add(1)
	}
}

func (m *WebhookMetrics) ObserveUnauthorized() {
	if m != nil {
		m.unauthorized.Add(1)
	}
}

func (m *WebhookMetrics) ObserveBadRequest() {
	if m != nil {
		m.badRequest.Add(1)
	}
}

func (m *WebhookMetrics) ObserveDuplicate() {
	if m != nil {
		m.duplicates.Add(1)
	}
}

func (m *WebhookMetrics) ObserveEnqueueFailure() {
	if m != nil {
		m.enqueueFailures.Add(1)
	}
}

func (m *WebhookMetrics) ObserveAccepted() {
	if m != nil {
		m.accepted.Add(1)
	}
}

func (m *WebhookMetrics) ObserveDecodeLatency(d time.Duration) {
	if m != nil && m.decodeLatency != nil {
		m.decodeLatency.Observe(d.Seconds())
	}
}

func (m *WebhookMetrics) ObserveHandlerDuration(d time.Duration) {
	if m != nil && m.handlerDuration != nil {
		m.handlerDuration.Observe(d.Seconds())
	}
}

func (m *WebhookMetrics) Expose(w io.Writer) bool {
	if m == nil {
		return true
	}

	n := func(suffix string) string { return m.prefix + "_webhook_" + suffix }

	counters := []struct {
		name  string
		help  string
		value uint64
	}{
		{n("requests_total"), "Total number of inbound webhook requests.", m.requests.Load()},
		{n("unauthorized_total"), "Total number of webhook requests rejected due to token mismatch.", m.unauthorized.Load()},
		{n("bad_request_total"), "Total number of webhook requests rejected due to invalid JSON.", m.badRequest.Load()},
		{n("duplicates_total"), "Total number of duplicate webhook requests short-circuited by deduplication.", m.duplicates.Load()},
		{n("enqueue_failures_total"), "Total number of webhook requests rejected because durable admission failed.", m.enqueueFailures.Load()},
		{n("accepted_total"), "Total number of webhook requests accepted for asynchronous processing.", m.accepted.Load()},
	}
	for _, c := range counters {
		if !WriteCounter(w, c.name, c.help, c.value) {
			return false
		}
	}

	histograms := []struct {
		name string
		help string
		hist *Histogram
	}{
		{n("handler_duration_seconds"), "Webhook durable admission duration in seconds.", m.handlerDuration},
		{n("decode_latency_seconds"), "Webhook request JSON decode latency in seconds.", m.decodeLatency},
	}
	for _, h := range histograms {
		if h.hist == nil {
			continue
		}

		if !WriteHistogram(w, h.name, h.help, h.hist.Snapshot()) {
			return false
		}
	}

	return true
}
