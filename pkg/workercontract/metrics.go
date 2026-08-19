package workercontract

import (
	"fmt"
	"io"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetricType은 Prometheus family type을 고정한다.
type MetricType string

const (
	MetricGauge   MetricType = "gauge"
	MetricCounter MetricType = "counter"
)

// MetricSample은 deterministic label set과 값을 담는다.
type MetricSample struct {
	Labels map[string]string
	Value  float64
}

// MetricFamily는 collector와 text writer가 공유하는 descriptor다.
type MetricFamily struct {
	Name    string
	Help    string
	Type    MetricType
	Samples []MetricSample
}

type metricDescriptor struct {
	name       string
	help       string
	metricType MetricType
}

var metricDescriptors = []metricDescriptor{
	{"iris_stack_worker_profile_info", "Loaded stack worker profile identity.", MetricGauge},
	{"iris_stack_worker_profile_file_match", "Whether current profile file bytes match the loaded profile.", MetricGauge},
	{"iris_stack_worker_profile_file_last_check_timestamp_seconds", "Unix timestamp of the last profile file comparison.", MetricGauge},
	{"iris_stack_worker_policy_info", "Configured attempt timeout and queue age policy modes.", MetricGauge},
	{"iris_stack_worker_enabled", "Whether the worker executor is enabled by the profile.", MetricGauge},
	{"iris_stack_worker_configured_workers", "Configured executor concurrency for this process.", MetricGauge},
	{"iris_stack_worker_running_workers", "Currently running executor workers in this process.", MetricGauge},
	{"iris_stack_worker_attempt_timeout_seconds", "Fixed attempt timeout in seconds.", MetricGauge},
	{"iris_stack_worker_max_queue_age_seconds", "Fixed maximum queue age in seconds.", MetricGauge},
	{"iris_stack_worker_queue_bounded", "Whether the canonical queue has a bounded capacity.", MetricGauge},
	{"iris_stack_worker_queue_capacity", "Bounded canonical queue capacity in items.", MetricGauge},
	{"iris_stack_worker_queue_depth", "Current ready canonical queue depth.", MetricGauge},
	{"iris_stack_worker_in_flight", "Attempts currently executing in this process.", MetricGauge},
	{"iris_stack_worker_oldest_queued_age_seconds", "Age of the oldest ready queue item in seconds.", MetricGauge},
	{"iris_stack_worker_oldest_in_flight_age_seconds", "Age of the oldest executing attempt in seconds.", MetricGauge},
	{"iris_stack_worker_queue_snapshot_success", "Whether the latest canonical queue snapshot succeeded.", MetricGauge},
	{"iris_stack_worker_queue_snapshot_last_success_timestamp_seconds", "Unix timestamp of the last successful canonical queue snapshot.", MetricGauge},
	{"iris_stack_worker_admissions_total", "Canonical queue admissions by ownership result.", MetricCounter},
	{"iris_stack_worker_attempts_total", "Started worker attempts by terminal outcome.", MetricCounter},
	{"iris_stack_worker_discarded_total", "Owned work discarded before attempt start by reason.", MetricCounter},
}

// Metrics는 현재 diagnostics를 exact metric vocabulary로 변환한다.
func (r *Registry) Metrics(observedAt time.Time) ([]MetricFamily, error) {
	envelope, err := r.Diagnostics(observedAt)
	if err != nil {
		return nil, err
	}
	families := make([]MetricFamily, len(metricDescriptors))
	byName := make(map[string]*MetricFamily, len(metricDescriptors))
	for index, descriptor := range metricDescriptors {
		families[index] = MetricFamily{Name: descriptor.name, Help: descriptor.help, Type: descriptor.metricType}
		byName[descriptor.name] = &families[index]
	}
	processLabels := map[string]string{
		"stack_service": envelope.Service,
		"stack_role":    envelope.Role,
		"runtime":       string(r.runtime),
	}
	profileLabels := cloneLabels(processLabels)
	profileLabels["contract_version"] = strconv.Itoa(ContractVersion)
	profileLabels["profile_id"] = envelope.Profile.ID
	profileLabels["profile_hash"] = envelope.Profile.Hash
	addSample(byName, "iris_stack_worker_profile_info", profileLabels, 1)
	addSample(byName, "iris_stack_worker_profile_file_match", processLabels, boolFloat(envelope.Profile.FileMatch))
	addSample(byName, "iris_stack_worker_profile_file_last_check_timestamp_seconds", processLabels, millisecondsToSeconds(envelope.Profile.FileCheckedAtEpochMS))

	workerIDs := make([]string, 0, len(envelope.Workers))
	for workerID := range envelope.Workers {
		workerIDs = append(workerIDs, workerID)
	}
	sort.Strings(workerIDs)
	for _, workerID := range workerIDs {
		diagnostics := envelope.Workers[workerID]
		profileWorker := r.loaded.Profile.Workers[workerID]
		labels := map[string]string{
			"stack_service": envelope.Service,
			"stack_role":    envelope.Role,
			"worker":        workerID,
			"runtime":       string(diagnostics.Runtime),
			"queue_backend": string(diagnostics.Queue.Backend),
			"queue_scope":   string(diagnostics.Queue.Scope),
		}
		policyLabels := cloneLabels(labels)
		policyLabels["attempt_timeout_mode"] = string(profileWorker.Executor.AttemptTimeout.Mode)
		policyLabels["queue_age_mode"] = string(profileWorker.Queue.MaxAge.Mode)
		addSample(byName, "iris_stack_worker_policy_info", policyLabels, 1)
		addSample(byName, "iris_stack_worker_enabled", labels, boolFloat(diagnostics.Executor.Enabled))
		addSample(byName, "iris_stack_worker_configured_workers", labels, float64(diagnostics.Executor.ConfiguredWorkers))
		addSample(byName, "iris_stack_worker_running_workers", labels, float64(diagnostics.Executor.RunningWorkers))
		if profileWorker.Executor.AttemptTimeout.Mode == DurationModeFixed {
			addSample(byName, "iris_stack_worker_attempt_timeout_seconds", labels, millisecondsToSeconds(*profileWorker.Executor.AttemptTimeout.Milliseconds))
		}
		if profileWorker.Queue.MaxAge.Mode == DurationModeFixed {
			addSample(byName, "iris_stack_worker_max_queue_age_seconds", labels, millisecondsToSeconds(*profileWorker.Queue.MaxAge.Milliseconds))
		}
		addSample(byName, "iris_stack_worker_queue_bounded", labels, boolFloat(diagnostics.Queue.Bounded))
		if diagnostics.Queue.Capacity != nil {
			addSample(byName, "iris_stack_worker_queue_capacity", labels, float64(*diagnostics.Queue.Capacity))
		}
		if diagnostics.Queue.SnapshotStatus == QueueSnapshotCurrent {
			addSample(byName, "iris_stack_worker_queue_depth", labels, float64(*diagnostics.Queue.Depth))
			addSample(byName, "iris_stack_worker_oldest_queued_age_seconds", labels, millisecondsToSeconds(*diagnostics.Queue.OldestQueuedAgeMS))
		}
		addSample(byName, "iris_stack_worker_in_flight", labels, float64(diagnostics.Executor.InFlight))
		addSample(byName, "iris_stack_worker_oldest_in_flight_age_seconds", labels, millisecondsToSeconds(diagnostics.Executor.OldestInFlightAgeMS))
		addSample(byName, "iris_stack_worker_queue_snapshot_success", labels, boolFloat(diagnostics.Queue.SnapshotStatus == QueueSnapshotCurrent))
		if diagnostics.Queue.LastSuccessAtEpochMS != nil {
			addSample(byName, "iris_stack_worker_queue_snapshot_last_success_timestamp_seconds", labels, millisecondsToSeconds(*diagnostics.Queue.LastSuccessAtEpochMS))
		}
		addAdmissionSamples(byName, labels, diagnostics.Totals.Admissions)
		addAttemptSamples(byName, labels, diagnostics.Totals.Attempts)
		addDiscardSamples(byName, labels, diagnostics.Totals.Discarded)
	}
	return families, nil
}

func addAdmissionSamples(families map[string]*MetricFamily, labels map[string]string, totals AdmissionTotals) {
	for _, entry := range []struct {
		result string
		value  uint64
	}{{"accepted", totals.Accepted}, {"duplicate", totals.Duplicate}, {"rejected", totals.Rejected}, {"failed", totals.Failed}, {"outcome_unknown", totals.OutcomeUnknown}} {
		resultLabels := cloneLabels(labels)
		resultLabels["result"] = entry.result
		addSample(families, "iris_stack_worker_admissions_total", resultLabels, float64(entry.value))
	}
}

func addAttemptSamples(families map[string]*MetricFamily, labels map[string]string, totals AttemptTotals) {
	for _, entry := range []struct {
		outcome string
		value   uint64
	}{{"success", totals.Success}, {"failed", totals.Failed}, {"timeout", totals.Timeout}, {"canceled", totals.Canceled}, {"panic", totals.Panic}, {"outcome_unknown", totals.OutcomeUnknown}} {
		outcomeLabels := cloneLabels(labels)
		outcomeLabels["outcome"] = entry.outcome
		addSample(families, "iris_stack_worker_attempts_total", outcomeLabels, float64(entry.value))
	}
}

func addDiscardSamples(families map[string]*MetricFamily, labels map[string]string, totals DiscardTotals) {
	for _, entry := range []struct {
		reason string
		value  uint64
	}{{"stale", totals.Stale}, {"shutdown", totals.Shutdown}} {
		reasonLabels := cloneLabels(labels)
		reasonLabels["reason"] = entry.reason
		addSample(families, "iris_stack_worker_discarded_total", reasonLabels, float64(entry.value))
	}
}

func addSample(families map[string]*MetricFamily, family string, labels map[string]string, value float64) {
	families[family].Samples = append(families[family].Samples, MetricSample{Labels: cloneLabels(labels), Value: value})
}

func cloneLabels(labels map[string]string) map[string]string {
	clone := make(map[string]string, len(labels)+1)
	maps.Copy(clone, labels)
	return clone
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func millisecondsToSeconds(value int64) float64 { return float64(value) / 1000 }

// WritePrometheus는 descriptor와 sample을 deterministic text exposition으로 쓴다.
func WritePrometheus(writer io.Writer, families []MetricFamily) error {
	for _, family := range families {
		if _, err := fmt.Fprintf(writer, "# HELP %s %s\n# TYPE %s %s\n", family.Name, sanitizeHelp(family.Help), family.Name, family.Type); err != nil {
			return err
		}
		for _, sample := range family.Samples {
			if _, err := fmt.Fprintf(writer, "%s%s %s\n", family.Name, formatMetricLabels(sample.Labels), strconv.FormatFloat(sample.Value, 'g', -1, 64)); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatMetricLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	builder.WriteByte('{')
	for index, name := range names {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(name)
		builder.WriteString("=\"")
		builder.WriteString(escapeLabel(labels[name]))
		builder.WriteByte('"')
	}
	builder.WriteByte('}')
	return builder.String()
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func sanitizeHelp(help string) string {
	help = strings.ReplaceAll(help, `\`, `\\`)
	return strings.ReplaceAll(help, "\n", `\n`)
}
