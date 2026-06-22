package obsmetrics

import (
	"io"
	"strconv"
	"sync/atomic"
	"time"
)

// WebhookLatencyBuckets는 ms~분 범위를 덮는 초 단위 히스토그램 경계입니다.
var WebhookLatencyBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
}

// WebhookDiagnostics는 webhook 워커/큐 진단 스냅샷을 메트릭으로 노출하기 위한 운영 값입니다.
type WebhookDiagnostics struct {
	WorkersConfigured int
	QueueSize         int
	Pending           int
	InFlight          int
	EnqueueRejected   uint64
	QueueFullCount    uint64
	HandlerTimeouts   uint64
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
	queueDepth      atomic.Int64

	diagnosticsSource atomic.Pointer[func() WebhookDiagnostics]
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

func (m *WebhookMetrics) ObserveQueueDepth(depth int) {
	if m != nil {
		m.queueDepth.Store(int64(depth))
	}
}

func (m *WebhookMetrics) ObserveHandlerDuration(d time.Duration) {
	if m != nil && m.handlerDuration != nil {
		m.handlerDuration.Observe(d.Seconds())
	}
}

// SetDiagnosticsSource는 워커/큐 진단 스냅샷을 노출 시점에 읽어올 콜백을 등록합니다. nil이면 진단 게이지를 생략합니다.
func (m *WebhookMetrics) SetDiagnosticsSource(source func() WebhookDiagnostics) {
	if m == nil {
		return
	}

	if source == nil {
		m.diagnosticsSource.Store(nil)

		return
	}

	m.diagnosticsSource.Store(&source)
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

	if !WriteGauge(w, n("queue_depth"), "Current webhook worker queue depth.", strconv.FormatInt(m.queueDepth.Load(), 10)) {
		return false
	}

	return m.writeDiagnostics(w, n)
}

func (m *WebhookMetrics) writeDiagnostics(w io.Writer, n func(string) string) bool {
	source := m.diagnosticsSource.Load()
	if source == nil {
		return true
	}

	diag := (*source)()

	gauges := []struct {
		name  string
		help  string
		value int64
	}{
		{n("workers_configured"), "Configured webhook worker count.", int64(diag.WorkersConfigured)},
		{n("queue_size"), "Configured webhook queue size.", int64(diag.QueueSize)},
		{n("pending"), "Webhook requests queued and not yet started.", int64(diag.Pending)},
		{n("inflight"), "Webhook requests currently being handled.", int64(diag.InFlight)},
	}
	for _, g := range gauges {
		if !WriteGauge(w, g.name, g.help, strconv.FormatInt(g.value, 10)) {
			return false
		}
	}

	counters := []struct {
		name  string
		help  string
		value uint64
	}{
		{n("enqueue_rejected_total"), "Total webhook requests rejected at enqueue time.", diag.EnqueueRejected},
		{n("queue_full_total"), "Total webhook requests dropped because the queue was full.", diag.QueueFullCount},
		{n("handler_timeouts_total"), "Total webhook handler executions that timed out.", diag.HandlerTimeouts},
	}
	for _, c := range counters {
		if !WriteCounter(w, c.name, c.help, c.value) {
			return false
		}
	}

	return true
}
