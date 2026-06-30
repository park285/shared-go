package main

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
)

type metric int

const (
	metricNs metric = iota
	metricBytes
	metricAllocs
)

type Issue struct {
	Level      string
	Bench      string
	BenchClass string
	Metric     string
	Baseline   float64
	Candidate  float64
	Detail     string
}

func isFalseVal(v any) bool {
	b, ok := v.(bool)
	return ok && !b
}

func isWarnVal(v any) bool {
	s, ok := v.(string)
	return ok && s == "warn"
}

func mergedBudget(policy *omap, config *omap) map[string]any {
	className, _ := mustGet(config, "class").(string)
	dm := mapOf(defaultsOf(policy), className)
	budget := map[string]any{}
	if dm != nil {
		for _, k := range dm.keys {
			budget[k] = dm.vals[k]
		}
	}
	for _, field := range budgetFieldsList {
		if v, ok := config.get(field); ok {
			budget[field] = v
		}
	}
	return budget
}

func sampleMetric(s Sample, m metric) *float64 {
	switch m {
	case metricNs:
		v := s.Ns
		return &v
	case metricBytes:
		return s.Bytes
	case metricAllocs:
		return s.Allocs
	}
	return nil
}

func meanMetric(samples []Sample, m metric) (float64, bool) {
	var sum float64
	n := 0
	for _, s := range samples {
		if v := sampleMetric(s, m); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func percentDelta(baseline, candidate float64) float64 {
	if baseline == 0 {
		if candidate > 0 {
			return math.Inf(1)
		}
		return 0.0
	}
	return ((candidate - baseline) / baseline) * 100.0
}

func formatDeltaPercent(v float64) string {
	if math.IsInf(v, 1) {
		return "+inf"
	}
	return fmt.Sprintf("+%.2f", v)
}

func compareResults(policy *omap, selected selection, baseline, candidate *Results) []Issue {
	var issues []Issue
	noiseFloor := toFloat(mustGet(settingsOf(policy), "noise_floor_ns"))
	for _, e := range selected {
		pkgStr, _ := mustGet(e.config, "package").(string)
		key := benchKey{pkgStr, e.name}
		baseSamples := baseline.Samples[key]
		candSamples := candidate.Samples[key]
		budget := mergedBudget(policy, e.config)
		benchClass, _ := mustGet(e.config, "class").(string)

		baseNs, okB := meanMetric(baseSamples, metricNs)
		candNs, okC := meanMetric(candSamples, metricNs)
		if okB && okC && candNs > baseNs {
			if baseNs < noiseFloor {
				deltaNs := candNs - baseNs
				if deltaNs > noiseFloor {
					issues = append(issues, Issue{"violation", e.name, benchClass, "ns/op", baseNs, candNs,
						fmt.Sprintf("+%.3f ns exceeds absolute budget %.3f ns", deltaNs, noiseFloor)})
				}
			} else {
				deltaPercent := percentDelta(baseNs, candNs)
				limit := toFloat(budget["max_ns_regression_percent"])
				if deltaPercent > limit {
					issues = append(issues, Issue{"violation", e.name, benchClass, "ns/op", baseNs, candNs,
						fmt.Sprintf("%s%% exceeds budget %.2f%%", formatDeltaPercent(deltaPercent), limit)})
				}
			}
		}

		baseBytes, okBB := meanMetric(baseSamples, metricBytes)
		candBytes, okCB := meanMetric(candSamples, metricBytes)
		if okBB && okCB && candBytes > baseBytes {
			deltaPercent := percentDelta(baseBytes, candBytes)
			limit := toFloat(budget["max_bytes_regression_percent"])
			if deltaPercent > limit {
				issues = append(issues, Issue{"violation", e.name, benchClass, "B/op", baseBytes, candBytes,
					fmt.Sprintf("%s%% exceeds budget %.2f%%", formatDeltaPercent(deltaPercent), limit)})
			}
		}

		baseAllocs, okBA := meanMetric(baseSamples, metricAllocs)
		candAllocs, okCA := meanMetric(candSamples, metricAllocs)
		if okBA && okCA && candAllocs > baseAllocs {
			allow := budget["allow_alloc_increase"]
			if isFalseVal(allow) {
				issues = append(issues, Issue{"violation", e.name, benchClass, "allocs/op", baseAllocs, candAllocs,
					"allocation count increased"})
			} else if isWarnVal(allow) {
				issues = append(issues, Issue{"warning", e.name, benchClass, "allocs/op", baseAllocs, candAllocs,
					"allocation count increased"})
			}
		}
	}
	return issues
}

func checkMissing(results *Results, selected selection) []string {
	var missing []string
	for _, e := range selected {
		pkgStr, _ := mustGet(e.config, "package").(string)
		key := benchKey{pkgStr, e.name}
		if _, ok := results.Samples[key]; !ok {
			missing = append(missing, fmt.Sprintf("%s package=%s", e.name, pkgStr))
		}
	}
	return missing
}

func printIssues(issues []Issue) {
	for _, is := range issues {
		fmt.Printf("%s: %s class=%s metric=%s baseline=%.3f candidate=%.3f %s\n",
			is.Level, is.Bench, is.BenchClass, is.Metric, is.Baseline, is.Candidate, is.Detail)
	}
}

func filesWithMissingMetric(samples []Sample, m metric) []string {
	set := map[string]bool{}
	for _, s := range samples {
		if sampleMetric(s, m) == nil {
			set[s.File] = true
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

func requiredMetricDiagnostics(policy *omap, selected selection, label string, results *Results) (errors, warnings []string) {
	for _, e := range selected {
		pkgStr, _ := mustGet(e.config, "package").(string)
		key := benchKey{pkgStr, e.name}
		samples := results.Samples[key]
		if len(samples) == 0 {
			continue
		}
		budget := mergedBudget(policy, e.config)
		bytesFiles := filesWithMissingMetric(samples, metricBytes)
		if _, ok := budget["max_bytes_regression_percent"]; ok && len(bytesFiles) > 0 {
			errors = append(errors, fmt.Sprintf(
				"missing B/op for %s in %s file(s): %s; run benchmarks with -benchmem",
				e.name, label, strings.Join(bytesFiles, ", ")))
		}
		allocFiles := filesWithMissingMetric(samples, metricAllocs)
		if len(allocFiles) > 0 {
			message := fmt.Sprintf(
				"missing allocs/op for %s in %s file(s): %s; run benchmarks with -benchmem",
				e.name, label, strings.Join(allocFiles, ", "))
			allow := budget["allow_alloc_increase"]
			if isFalseVal(allow) {
				errors = append(errors, message)
			} else if isWarnVal(allow) {
				warnings = append(warnings, message)
			}
		}
	}
	return errors, warnings
}

func printRequiredMetricDiagnostics(policy *omap, selected selection, baseline, candidate *Results) bool {
	var allErrors, allWarnings []string
	if baseline != nil {
		errs, warns := requiredMetricDiagnostics(policy, selected, "baseline", baseline)
		allErrors = append(allErrors, errs...)
		allWarnings = append(allWarnings, warns...)
	}
	errs, warns := requiredMetricDiagnostics(policy, selected, "candidate", candidate)
	allErrors = append(allErrors, errs...)
	allWarnings = append(allWarnings, warns...)
	for _, w := range allWarnings {
		fmt.Printf("warning: %s\n", w)
	}
	for _, e := range allErrors {
		fmt.Fprintf(os.Stderr, "error: %s\n", e)
	}
	return len(allErrors) == 0
}
