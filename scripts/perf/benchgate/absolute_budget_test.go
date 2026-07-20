package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestSelectionHashPreservesLegacyPolicyWithoutAbsoluteBudgets(t *testing.T) {
	root, _, selected, args := setupCollectionFixture(t)
	_, got, _, _, err := hashesForEvidence(args.policy, root, selected)
	if err != nil {
		t.Fatal(err)
	}
	want := legacySelectionHash(selected)
	if got != want {
		t.Fatalf("selection hash = %s, want legacy hash %s", got, want)
	}

	selected[0].config.vals["max_ns_per_op"] = 100
	_, withAbsolute, _, _, err := hashesForEvidence(args.policy, root, selected)
	if err != nil {
		t.Fatal(err)
	}
	if withAbsolute == want {
		t.Fatal("absolute SLA did not change the selection hash")
	}
}

func legacySelectionHash(selected selection) string {
	ordered := append(selection(nil), selected...)
	slices.SortFunc(ordered, func(a, b benchEntry) int { return strings.Compare(a.name, b.name) })
	return canonicalHash(func(buffer *bytes.Buffer) {
		for _, entry := range ordered {
			hashString(buffer, entry.name)
			for _, field := range []string{
				"package", "class", "gate",
				"max_ns_regression_percent", "max_bytes_regression_percent", "allow_alloc_increase",
			} {
				value, _ := entry.config.get(field)
				hashString(buffer, fmt.Sprint(value))
			}
		}
		for _, path := range harnessFilesForSelection(selected) {
			hashString(buffer, path)
		}
	})
}

func TestAbsoluteBudgetsReplaceRelativeChecksPerMetric(t *testing.T) {
	policy, selected := loadAbsoluteBudgetPolicy(t, 200, 32, 4)
	baseline := benchmarkResults(100, 8, 1)
	candidate := benchmarkResults(150, 16, 3)

	if issues := compareResults(policy, selected, baseline, candidate); len(issues) != 0 {
		t.Fatalf("compareResults() issues = %#v, want none", issues)
	}
}

func TestAbsoluteBudgetsRejectCandidateOverEachLimit(t *testing.T) {
	policy, selected := loadAbsoluteBudgetPolicy(t, 200, 32, 4)
	baseline := benchmarkResults(100, 8, 1)
	tests := []struct {
		name   string
		result *Results
		metric string
	}{
		{name: "time", result: benchmarkResults(201, 16, 3), metric: "ns/op"},
		{name: "bytes", result: benchmarkResults(150, 33, 3), metric: "B/op"},
		{name: "allocations", result: benchmarkResults(150, 16, 5), metric: "allocs/op"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issues := compareResults(policy, selected, baseline, tc.result)
			if len(issues) != 1 || issues[0].Metric != tc.metric {
				t.Fatalf("compareResults() issues = %#v, want one %s violation", issues, tc.metric)
			}
		})
	}
}

func TestAbsoluteBudgetIssuesRejectInvalidBaselineEvidence(t *testing.T) {
	policy, selected := loadAbsoluteBudgetPolicy(t, 200, 32, 4)
	issues := absoluteBudgetIssues(policy, selected, benchmarkResults(201, 16, 3))
	if len(issues) != 1 || issues[0].Metric != "ns/op" {
		t.Fatalf("absoluteBudgetIssues() issues = %#v, want one ns/op violation", issues)
	}
}

func TestAbsoluteBudgetsMustBePositive(t *testing.T) {
	path := writeAbsoluteBudgetPolicy(t, 0, 32, 4)
	policy, err := loadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicy(policy, "fixture"); err == nil {
		t.Fatal("validatePolicy() error = nil, want non-positive absolute budget rejection")
	}
}

func loadAbsoluteBudgetPolicy(t *testing.T, ns, bytes, allocs int) (*omap, selection) {
	t.Helper()
	policy, err := loadPolicy(writeAbsoluteBudgetPolicy(t, ns, bytes, allocs))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicy(policy, "fixture"); err != nil {
		t.Fatal(err)
	}
	selected, selectionErr := selectedBenchmarks(policy, "pr")
	if selectionErr != nil {
		t.Fatal(selectionErr)
	}
	return policy, selected
}

func writeAbsoluteBudgetPolicy(t *testing.T, ns, bytes, allocs int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	text := "schemaVersion: 1\n" +
		"repo: fixture\n" +
		"defaults:\n" +
		"  critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n" +
		"  hotpath: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n" +
		"  build_path: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n" +
		"  non_critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n" +
		"benchmarks:\n" +
		"  BenchmarkTarget:\n" +
		"    package: ./fixture\n" +
		"    class: critical\n" +
		"    gate: pr\n" +
		"    harness_files: [fixture/bench_test.go]\n" +
		"    max_ns_per_op: " + strconv.Itoa(ns) + "\n" +
		"    max_bytes_per_op: " + strconv.Itoa(bytes) + "\n" +
		"    max_allocs_per_op: " + strconv.Itoa(allocs) + "\n" +
		"settings: { mode: fail, benchstat_alpha: 0.05, min_count: 1, noise_floor_ns: 0 }\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func benchmarkResults(ns, bytes, allocs float64) *Results {
	return &Results{Samples: map[benchKey][]Sample{
		{pkg: "./fixture", name: "BenchmarkTarget"}: {{Ns: ns, Bytes: &bytes, Allocs: &allocs}},
	}}
}
