package obsmetrics

import (
	"io"
	"sync/atomic"
	"time"
)

// WebhookLatencyBuckets는 ms~분 범위를 덮는 초 단위 히스토그램 경계입니다.
var WebhookLatencyBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
}

// WebhookMetrics는 iris-client-go webhook.Metrics 관측 포인트를 prefix 네임스페이스
// 단위로 구현한 공통 구현체입니다. 메서드 집합이 webhook.Metrics와 구조적으로 일치하므로
// iris-client-go를 import하지 않고도 iris.WithMetrics에 주입할 수 있습니다.
type WebhookMetrics struct {
	prefix string

	requests        atomic.Uint64
	unauthorized    atomic.Uint64
	badRequest      atomic.Uint64
	duplicates      atomic.Uint64
	enqueueFailures atomic.Uint64
	accepted        atomic.Uint64

	handlerDuration *Histogram
	enqueueWait     *Histogram
	decodeLatency   *Histogram
	dedupLatency    *Histogram
}

// NewWebhookMetrics는 prefix 네임스페이스(예: "chat_bot", "twentyq")로 webhook 메트릭을 만듭니다.
func NewWebhookMetrics(prefix string) *WebhookMetrics {
	return &WebhookMetrics{
		prefix:          prefix,
		handlerDuration: NewHistogram(WebhookLatencyBuckets),
		enqueueWait:     NewHistogram(WebhookLatencyBuckets),
		decodeLatency:   NewHistogram(WebhookLatencyBuckets),
		dedupLatency:    NewHistogram(WebhookLatencyBuckets),
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

func (m *WebhookMetrics) ObserveDedupLatency(d time.Duration) {
	if m != nil && m.dedupLatency != nil {
		m.dedupLatency.Observe(d.Seconds())
	}
}

func (m *WebhookMetrics) ObserveEnqueueWait(d time.Duration) {
	if m != nil && m.enqueueWait != nil {
		m.enqueueWait.Observe(d.Seconds())
	}
}

// ObserveQueueDepth는 iris-client-go webhook.Metrics 계약을 구현한다. Scheduler queue는
// Stack Worker Contract registry가 소유하므로 generic webhook metric으로 중복 노출하지 않는다.
func (m *WebhookMetrics) ObserveQueueDepth(_ int) {}

func (m *WebhookMetrics) ObserveHandlerDuration(d time.Duration) {
	if m != nil && m.handlerDuration != nil {
		m.handlerDuration.Observe(d.Seconds())
	}
}

// Expose는 <prefix>_webhook_* 메트릭을 Prometheus 텍스트 포맷으로 직렬화합니다.
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
		{n("enqueue_failures_total"), "Total number of webhook requests that could not be queued.", m.enqueueFailures.Load()},
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
		{n("handler_duration_seconds"), "Webhook message handler execution duration in seconds.", m.handlerDuration},
		{n("enqueue_wait_seconds"), "Time a webhook request waited before being enqueued, in seconds.", m.enqueueWait},
		{n("decode_latency_seconds"), "Webhook request JSON decode latency in seconds.", m.decodeLatency},
		{n("dedup_latency_seconds"), "Deduplication check latency in seconds.", m.dedupLatency},
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
