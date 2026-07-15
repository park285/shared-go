from __future__ import annotations

from pathlib import Path

ROOT = Path.cwd()


def replace_once(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected one replacement target, found {count}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")


def write(path: str, content: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


replace_once(
    "pkg/promptguard/rulepack_match.go",
    """\tprefilterText := text
\tif r.View == viewRaw {
\t\tprefilterText = segment.Views.Norm
\t}
\tif !containsAllLiteralGroups(prefilterText, r.RequiredLiteralGroups) {
\t\treturn \"\", 0, false
\t}
""",
    """\t// Required literals are compiled in the matcher's own representation. A raw
\t// rule can intentionally contain compatibility characters, provider syntax,
\t// or other bytes that normalize to a different surface. Prefiltering those
\t// rules against the normalized view can therefore create false negatives.
\t// Keep the optimization for normalized/joined views, but always execute raw
\t// matchers against the raw surface.
\tif r.View != viewRaw && !containsAllLiteralGroups(text, r.RequiredLiteralGroups) {
\t\treturn \"\", 0, false
\t}
""",
)

write(
    "pkg/promptguard/rulepack_raw_security_test.go",
    r'''package promptguard

import "testing"

func TestRawRulesDoNotUseNormalizedLiteralPrefilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rule rawRule
	}{
		{
			name: "regex compatibility characters",
			rule: rawRule{
				ID:             "raw-fullwidth-regex",
				Family:         "raw-fullwidth-regex",
				Type:           ruleTypeRegex,
				Action:         hitActionScore,
				View:           viewRaw,
				Segments:       []string{string(segmentPlain)},
				Pattern:        `ｓｙｓｔｅｍ`,
				Weight:         1,
				MaxOccurrences: 1,
			},
		},
		{
			name: "phrase compatibility characters",
			rule: rawRule{
				ID:             "raw-fullwidth-phrase",
				Family:         "raw-fullwidth-phrase",
				Type:           ruleTypePhrase,
				Action:         hitActionScore,
				View:           viewRaw,
				Segments:       []string{string(segmentPlain)},
				Phrases:        []string{"ｓｙｓｔｅｍ"},
				MatchMode:      phraseMatchSubstring,
				Weight:         1,
				MaxOccurrences: 1,
			},
		},
	}

	segment := textSegment{Kind: segmentPlain, Views: normalizeViews("ｓｙｓｔｅｍ")}
	if segment.Views.Raw == segment.Views.Norm {
		t.Fatal("test fixture must normalize to a different representation")
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			compiled, err := compileRule(&tc.rule)
			if err != nil {
				t.Fatalf("compileRule() error = %v", err)
			}
			if matches := compiled.matchSegment(segment, compilePolicy(&rawRulepack{Version: 3}), 1); len(matches) != 1 {
				t.Fatalf("matchSegment() matches = %d, want 1", len(matches))
			}
		})
	}
}
''',
)

write(
    "pkg/obsmetrics/vec.go",
    r'''package obsmetrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Labels는 Prometheus label name/value 집합입니다. 렌더링 시 label name 기준으로 정렬됩니다.
type Labels map[string]string

type labelPair struct {
	name  string
	value string
}

type seriesEntry[T any] struct {
	key    string
	labels []labelPair
	value  T
}

const (
	// DefaultMaxMetricSeries bounds the process-lifetime cardinality retained by
	// every vector created through the compatibility constructors.
	DefaultMaxMetricSeries = 1024
	// DefaultMaxMetricLabels bounds the number of labels in one series.
	DefaultMaxMetricLabels = 16
	// DefaultMaxMetricLabelNameBytes and DefaultMaxMetricLabelValueBytes bound
	// allocations performed while canonicalizing attacker-influenced labels.
	DefaultMaxMetricLabelNameBytes  = 128
	DefaultMaxMetricLabelValueBytes = 256
)

// VecOptions configures hard resource limits for a metric vector. Non-positive
// values select the safe defaults; there is intentionally no unbounded mode.
type VecOptions struct {
	MaxSeries          int
	MaxLabels          int
	MaxLabelNameBytes  int
	MaxLabelValueBytes int
}

type seriesLimiter struct {
	maxSeries          int64
	maxLabels          int
	maxLabelNameBytes  int
	maxLabelValueBytes int
	count              atomic.Int64
	dropped            atomic.Uint64
}

func newSeriesLimiter(options VecOptions) seriesLimiter {
	if options.MaxSeries <= 0 {
		options.MaxSeries = DefaultMaxMetricSeries
	}
	if options.MaxLabels <= 0 {
		options.MaxLabels = DefaultMaxMetricLabels
	}
	if options.MaxLabelNameBytes <= 0 {
		options.MaxLabelNameBytes = DefaultMaxMetricLabelNameBytes
	}
	if options.MaxLabelValueBytes <= 0 {
		options.MaxLabelValueBytes = DefaultMaxMetricLabelValueBytes
	}

	return seriesLimiter{
		maxSeries:          int64(options.MaxSeries),
		maxLabels:          options.MaxLabels,
		maxLabelNameBytes:  options.MaxLabelNameBytes,
		maxLabelValueBytes: options.MaxLabelValueBytes,
	}
}

func (l *seriesLimiter) canonicalize(labels Labels) (string, []labelPair, bool) {
	if len(labels) > l.maxLabels {
		l.dropped.Add(1)
		return "", nil, false
	}
	for name, value := range labels {
		if len(name) > l.maxLabelNameBytes || len(value) > l.maxLabelValueBytes {
			l.dropped.Add(1)
			return "", nil, false
		}
	}

	key, pairs := canonicalLabels(labels)
	return key, pairs, true
}

func (l *seriesLimiter) reserve() bool {
	for {
		current := l.count.Load()
		if current >= l.maxSeries {
			l.dropped.Add(1)
			return false
		}
		if l.count.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (l *seriesLimiter) release() {
	l.count.Add(-1)
}

func (l *seriesLimiter) seriesCount() int {
	return int(l.count.Load())
}

func (l *seriesLimiter) droppedSeries() uint64 {
	return l.dropped.Load()
}

func loadOrCreateSeries[T any](
	series *sync.Map,
	limiter *seriesLimiter,
	key string,
	labels []labelPair,
	create func() T,
) (T, bool) {
	var zero T
	if actual, ok := series.Load(key); ok {
		stored, typed := actual.(*seriesEntry[T])
		if !typed {
			return zero, false
		}
		return stored.value, true
	}
	if !limiter.reserve() {
		return zero, false
	}

	entry := &seriesEntry[T]{key: key, labels: labels, value: create()}
	actual, loaded := series.LoadOrStore(key, entry)
	if loaded {
		limiter.release()
	}
	stored, ok := actual.(*seriesEntry[T])
	if !ok {
		if !loaded {
			series.Delete(key)
			limiter.release()
		}
		return zero, false
	}
	return stored.value, true
}

// CounterVec는 동일 metric name/help를 공유하는 label-set별 counter 집합입니다.
type CounterVec struct {
	name    string
	help    string
	series  sync.Map // map[string]*seriesEntry[*Counter]
	limiter seriesLimiter
}

// Counter는 원자적으로 증가하는 단일 counter series입니다.
type Counter struct {
	value atomic.Uint64
}

func NewCounterVec(name, help string) *CounterVec {
	return NewCounterVecWithOptions(name, help, VecOptions{})
}

func NewCounterVecWithOptions(name, help string, options VecOptions) *CounterVec {
	return &CounterVec{name: name, help: help, limiter: newSeriesLimiter(options)}
}

func (v *CounterVec) With(labels Labels) *Counter {
	if v == nil {
		return nil
	}
	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}
	counter, ok := loadOrCreateSeries(&v.series, &v.limiter, key, labelPairs, func() *Counter { return &Counter{} })
	if !ok {
		return nil
	}
	return counter
}

func (v *CounterVec) Inc(labels Labels) {
	v.Add(labels, 1)
}

func (v *CounterVec) Add(labels Labels, delta uint64) {
	counter := v.With(labels)
	if counter != nil {
		counter.Add(delta)
	}
}

func (v *CounterVec) SeriesCount() int {
	if v == nil {
		return 0
	}
	return v.limiter.seriesCount()
}

func (v *CounterVec) DroppedSeries() uint64 {
	if v == nil {
		return 0
	}
	return v.limiter.droppedSeries()
}

func (v *CounterVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "counter") {
		return false
	}
	for _, entry := range collectSeries[*Counter](&v.series) {
		if !writeMetricSample(w, v.name, entry.labels, strconv.FormatUint(entry.value.Value(), 10)) {
			return false
		}
	}
	return true
}

func (c *Counter) Inc() {
	c.Add(1)
}

func (c *Counter) Add(delta uint64) {
	if c != nil {
		c.value.Add(delta)
	}
}

func (c *Counter) Value() uint64 {
	if c == nil {
		return 0
	}
	return c.value.Load()
}

// GaugeVec는 동일 metric name/help를 공유하는 label-set별 float64 gauge 집합입니다.
type GaugeVec struct {
	name    string
	help    string
	series  sync.Map // map[string]*seriesEntry[*Gauge]
	limiter seriesLimiter
}

// Gauge는 원자적으로 설정되는 단일 gauge series입니다.
type Gauge struct {
	bits atomic.Uint64
}

func NewGaugeVec(name, help string) *GaugeVec {
	return NewGaugeVecWithOptions(name, help, VecOptions{})
}

func NewGaugeVecWithOptions(name, help string, options VecOptions) *GaugeVec {
	return &GaugeVec{name: name, help: help, limiter: newSeriesLimiter(options)}
}

func (v *GaugeVec) With(labels Labels) *Gauge {
	if v == nil {
		return nil
	}
	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}
	gauge, ok := loadOrCreateSeries(&v.series, &v.limiter, key, labelPairs, func() *Gauge { return &Gauge{} })
	if !ok {
		return nil
	}
	return gauge
}

func (v *GaugeVec) Set(labels Labels, value float64) {
	gauge := v.With(labels)
	if gauge != nil {
		gauge.Set(value)
	}
}

func (v *GaugeVec) SeriesCount() int {
	if v == nil {
		return 0
	}
	return v.limiter.seriesCount()
}

func (v *GaugeVec) DroppedSeries() uint64 {
	if v == nil {
		return 0
	}
	return v.limiter.droppedSeries()
}

func (v *GaugeVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "gauge") {
		return false
	}
	for _, entry := range collectSeries[*Gauge](&v.series) {
		if !writeMetricSample(w, v.name, entry.labels, strconv.FormatFloat(entry.value.Value(), 'g', -1, 64)) {
			return false
		}
	}
	return true
}

func (g *Gauge) Set(value float64) {
	if g != nil {
		g.bits.Store(math.Float64bits(value))
	}
}

func (g *Gauge) Value() float64 {
	if g == nil {
		return 0
	}
	return math.Float64frombits(g.bits.Load())
}

// HistogramVec는 동일 metric name/help/bucket을 공유하는 label-set별 histogram 집합입니다.
type HistogramVec struct {
	name    string
	help    string
	buckets []float64
	series  sync.Map // map[string]*seriesEntry[*Histogram]
	limiter seriesLimiter
}

func NewHistogramVec(name, help string, buckets []float64) *HistogramVec {
	return NewHistogramVecWithOptions(name, help, buckets, VecOptions{})
}

func NewHistogramVecWithOptions(name, help string, buckets []float64, options VecOptions) *HistogramVec {
	bounds := make([]float64, len(buckets))
	copy(bounds, buckets)
	return &HistogramVec{name: name, help: help, buckets: bounds, limiter: newSeriesLimiter(options)}
}

func (v *HistogramVec) With(labels Labels) *Histogram {
	if v == nil {
		return nil
	}
	key, labelPairs, ok := v.limiter.canonicalize(labels)
	if !ok {
		return nil
	}
	histogram, ok := loadOrCreateSeries(&v.series, &v.limiter, key, labelPairs, func() *Histogram {
		return NewHistogram(v.buckets)
	})
	if !ok {
		return nil
	}
	return histogram
}

func (v *HistogramVec) Observe(labels Labels, value float64) {
	hist := v.With(labels)
	if hist != nil {
		hist.Observe(value)
	}
}

func (v *HistogramVec) SeriesCount() int {
	if v == nil {
		return 0
	}
	return v.limiter.seriesCount()
}

func (v *HistogramVec) DroppedSeries() uint64 {
	if v == nil {
		return 0
	}
	return v.limiter.droppedSeries()
}

func (v *HistogramVec) WriteExposition(w io.Writer) bool {
	if v == nil {
		return true
	}
	if !writeMetricHeader(w, v.name, v.help, "histogram") {
		return false
	}
	for _, entry := range collectSeries[*Histogram](&v.series) {
		snap := entry.value.Snapshot()
		if len(snap.UpperBounds) != len(snap.Cumulative) {
			return false
		}
		for i, ub := range snap.UpperBounds {
			bucketLabels := appendLabel(entry.labels, labelPair{name: "le", value: strconv.FormatFloat(ub, 'g', -1, 64)})
			if !writeMetricSample(w, v.name+"_bucket", bucketLabels, strconv.FormatUint(snap.Cumulative[i], 10)) {
				return false
			}
		}
		infLabels := appendLabel(entry.labels, labelPair{name: "le", value: "+Inf"})
		if !writeMetricSample(w, v.name+"_bucket", infLabels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}
		if !writeMetricSample(w, v.name+"_sum", entry.labels, strconv.FormatFloat(snap.Sum, 'g', -1, 64)) {
			return false
		}
		if !writeMetricSample(w, v.name+"_count", entry.labels, strconv.FormatUint(snap.Total, 10)) {
			return false
		}
	}
	return true
}

func collectSeries[T any](series *sync.Map) []*seriesEntry[T] {
	entries := make([]*seriesEntry[T], 0)
	series.Range(func(_, value any) bool {
		if entry, ok := value.(*seriesEntry[T]); ok {
			entries = append(entries, entry)
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})
	return entries
}

func canonicalLabels(labels Labels) (string, []labelPair) {
	pairs := labelsFromMap(labels)
	var key strings.Builder
	for _, pair := range pairs {
		_, _ = fmt.Fprintf(&key, "%d:%s=%d:%s;", len(pair.name), pair.name, len(pair.value), pair.value)
	}
	return key.String(), pairs
}

func labelsFromMap(labels Labels) []labelPair {
	if len(labels) == 0 {
		return nil
	}
	pairs := make([]labelPair, 0, len(labels))
	for name, value := range labels {
		pairs = append(pairs, labelPair{name: name, value: value})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].name < pairs[j].name
	})
	return pairs
}
''',
)

write(
    "pkg/obsmetrics/vec_security_test.go",
    r'''package obsmetrics

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestMetricVectorsEnforceSeriesLimit(t *testing.T) {
	t.Parallel()

	options := VecOptions{MaxSeries: 2}
	counter := NewCounterVecWithOptions("requests_total", "Requests", options)
	gauge := NewGaugeVecWithOptions("active", "Active", options)
	histogram := NewHistogramVecWithOptions("latency", "Latency", []float64{1}, options)

	for i := 0; i < 3; i++ {
		labels := Labels{"id": fmt.Sprintf("%d", i)}
		counter.Inc(labels)
		gauge.Set(labels, float64(i))
		histogram.Observe(labels, float64(i))
	}

	for name, got := range map[string]int{
		"counter":   counter.SeriesCount(),
		"gauge":     gauge.SeriesCount(),
		"histogram": histogram.SeriesCount(),
	} {
		if got != 2 {
			t.Fatalf("%s SeriesCount() = %d, want 2", name, got)
		}
	}
	if counter.DroppedSeries() != 1 || gauge.DroppedSeries() != 1 || histogram.DroppedSeries() != 1 {
		t.Fatalf("DroppedSeries() = (%d, %d, %d), want (1, 1, 1)", counter.DroppedSeries(), gauge.DroppedSeries(), histogram.DroppedSeries())
	}
}

func TestMetricVectorRejectsOversizedLabelsBeforeRetention(t *testing.T) {
	t.Parallel()

	vec := NewCounterVecWithOptions("requests_total", "Requests", VecOptions{
		MaxSeries:          8,
		MaxLabels:          1,
		MaxLabelNameBytes:  4,
		MaxLabelValueBytes: 4,
	})
	vec.Inc(Labels{"a": "1", "b": "2"})
	vec.Inc(Labels{"long-name": "1"})
	vec.Inc(Labels{"name": strings.Repeat("x", 5)})

	if got := vec.SeriesCount(); got != 0 {
		t.Fatalf("SeriesCount() = %d, want 0", got)
	}
	if got := vec.DroppedSeries(); got != 3 {
		t.Fatalf("DroppedSeries() = %d, want 3", got)
	}
}

func TestMetricVectorConcurrentAdmissionNeverExceedsLimit(t *testing.T) {
	t.Parallel()

	const limit = 8
	vec := NewCounterVecWithOptions("requests_total", "Requests", VecOptions{MaxSeries: limit})
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
		}(i)
	}
	wg.Wait()

	if got := vec.SeriesCount(); got != limit {
		t.Fatalf("SeriesCount() = %d, want %d", got, limit)
	}
	if got := vec.DroppedSeries(); got != 128-limit {
		t.Fatalf("DroppedSeries() = %d, want %d", got, 128-limit)
	}
}

func TestMetricVectorCompatibilityConstructorUsesSafeDefault(t *testing.T) {
	t.Parallel()

	vec := NewCounterVec("requests_total", "Requests")
	for i := 0; i <= DefaultMaxMetricSeries; i++ {
		vec.Inc(Labels{"id": fmt.Sprintf("%d", i)})
	}
	if got := vec.SeriesCount(); got != DefaultMaxMetricSeries {
		t.Fatalf("SeriesCount() = %d, want %d", got, DefaultMaxMetricSeries)
	}
	if got := vec.DroppedSeries(); got != 1 {
		t.Fatalf("DroppedSeries() = %d, want 1", got)
	}
}
''',
)

replace_once(
    "pkg/logging/sanitize.go",
    'var sensitiveExactKeys = map[string]struct{}{\n\t"token":            {},',
    'var sensitiveExactKeys = map[string]struct{}{\n\t"key":              {},\n\t"token":            {},',
)
replace_once(
    "pkg/logging/sanitize_test.go",
    '\t\t{"key", "key", "apikey123", false},',
    '\t\t{"key", "key", "apikey123", true},',
)

replace_once(
    "scripts/ci/check-workflow-secrets.sh",
    """        kv = parse_key_value(raw)
        if kv is not None:
            _, value = kv
            normalized = unquote_scalar(value)
""",
    """        kv = parse_key_value(raw)
        if kv is not None:
            key, value = kv
            source = strip_yaml_comment(raw).strip()
            if source.startswith("- "):
                source = source[2:].strip()
            if source[:1] in {"'", '"'} and key.lower() in {
                "permissions", "secrets", "jobs", "uses", "with", "persist-credentials"
            }:
                failures.append((line_no, f"quoted policy-sensitive YAML key {key!r} is unsupported"))
            normalized = unquote_scalar(value)
""",
)
replace_once(
    "scripts/ci/check-workflow-secrets.sh",
    """        line = strip_yaml_comment(raw)
        match = re.match(r"^\\s*([A-Za-z0-9_-]+)\\s*:\\s*([A-Za-z0-9_-]+)\\s*$", line)
        if not match:
            continue
        key = match.group(1)
        if key in seen:
            return False
        seen.add(key)
        saw_entry = True
        if match.group(2) not in {"read", "none"}:
            return False
""",
    """        parsed = parse_key_value(raw)
        if parsed is None:
            return False
        key, raw_value = parsed
        value = unquote_scalar(raw_value)
        if not re.fullmatch(r"[A-Za-z0-9_-]+", value):
            return False
        if key in seen:
            return False
        seen.add(key)
        saw_entry = True
        if value not in {"read", "none"}:
            return False
""",
)

replace_once(
    "scripts/ci/check-workflow-secrets_test.sh",
    """if (( failures > 0 )); then
""",
    r'''quoted_permissions="${TMP_DIR}/quoted-permissions.yml"
write_workflow "${quoted_permissions}" \
  "name: quoted-permissions" \
  "on:" \
  "  pull_request:" \
  '"permissions":' \
  "  contents: write" \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "quoted permissions key fails closed" "quoted policy-sensitive YAML key" "${quoted_permissions}"

quoted_secrets="${TMP_DIR}/quoted-secrets.yml"
write_workflow "${quoted_secrets}" \
  "name: quoted-secrets" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  "jobs:" \
  "  call:" \
  "    uses: owner/repo/.github/workflows/reusable.yml@main" \
  '    "secrets": inherit'
expect_failure "quoted secrets key fails closed" "quoted policy-sensitive YAML key" "${quoted_secrets}"

quoted_permission_scope="${TMP_DIR}/quoted-permission-scope.yml"
write_workflow "${quoted_permission_scope}" \
  "name: quoted-permission-scope" \
  "on:" \
  "  pull_request:" \
  "permissions:" \
  "  contents: read" \
  '  "pull-requests": write' \
  "jobs:" \
  "  test:" \
  "    runs-on: ubuntu-latest" \
  "    steps:" \
  "      - run: echo ok"
expect_failure "quoted permission scope cannot hide write" "read-only permissions" "${quoted_permission_scope}"

if (( failures > 0 )); then
''',
)

replace_once(
    ".github/workflows/ci.yml",
    """      - name: Workflow secret boundary
        run: bash scripts/ci/check-workflow-secrets.sh

      - name: SQL ownership
""",
    """      - name: Workflow secret boundary
        run: bash scripts/ci/check-workflow-secrets.sh

      - name: Workflow secret boundary tests
        run: bash scripts/ci/check-workflow-secrets_test.sh

      - name: SQL ownership
""",
)

# The helper files must not appear in the final pull-request tree.
(ROOT / ".github/workflows/agent-security-patch.yml").unlink()
Path(__file__).unlink()
