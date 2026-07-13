package main

import (
	"slices"
	"strconv"
	"strings"
)

const DefaultBenchtime = "100ms"

var topFieldsSet = map[string]bool{
	"schemaVersion": true, "repo": true, "defaults": true, "benchmarks": true, "settings": true,
}
var classesSet = map[string]bool{
	"critical": true, "hotpath": true, "build_path": true, "non_critical": true,
}
var classesList = []string{"critical", "hotpath", "build_path", "non_critical"}
var budgetFieldsSet = map[string]bool{
	"max_ns_regression_percent": true, "max_bytes_regression_percent": true, "allow_alloc_increase": true,
}
var budgetFieldsList = []string{"max_ns_regression_percent", "max_bytes_regression_percent", "allow_alloc_increase"}
var benchFieldsSet = map[string]bool{
	"max_ns_regression_percent": true, "max_bytes_regression_percent": true, "allow_alloc_increase": true,
	"package": true, "class": true, "gate": true, "harness_files": true,
}
var settingsFieldsSet = map[string]bool{
	"mode": true, "benchstat_alpha": true, "min_count": true, "noise_floor_ns": true,
}
var settingsFieldsList = []string{"mode", "benchstat_alpha", "min_count", "noise_floor_ns"}
var gatesSet = map[string]bool{"pr": true, "nightly": true, "release": true}

type benchEntry struct {
	name   string
	config *omap
}

type selection []benchEntry

func unknownFields(m *omap, allowed map[string]bool, path string) error {
	for _, k := range m.keys {
		if !allowed[k] {
			return perr("unknown field: %s.%s", path, k)
		}
	}
	return nil
}

func requireMapping(v any, path string) (*omap, error) {
	m, ok := v.(*omap)
	if !ok {
		return nil, perr("%s must be a mapping", path)
	}
	return m, nil
}

func requireNumber(v any, path string) (float64, error) {
	switch n := v.(type) {
	case bool:
		return 0, perr("%s must be a number", path)
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case float64:
		return n, nil
	}
	return 0, perr("%s must be a number", path)
}

func validateAllocPolicy(v any, path string) error {
	if isFalseVal(v) || isWarnVal(v) {
		return nil
	}
	return perr("%s must be false or warn", path)
}

func validatePolicy(policy *omap, repoName string) error {
	if err := unknownFields(policy, topFieldsSet, "policy"); err != nil {
		return err
	}
	sv, _ := policy.get("schemaVersion")
	if !equalsInt1(sv) {
		return perr("schemaVersion must be 1")
	}
	repoV, repoOk := policy.get("repo")
	if !stringEquals(repoV, repoName) {
		return perr("repo mismatch: policy repo=%s running repo=%s", pyValStr(repoV, repoOk), repoName)
	}

	defaultsV, _ := policy.get("defaults")
	defaults, err := requireMapping(defaultsV, "defaults")
	if err != nil {
		return err
	}
	if err := unknownFields(defaults, classesSet, "defaults"); err != nil {
		return err
	}
	for _, cls := range classesList {
		cfgV, ok := defaults.get(cls)
		if !ok {
			return perr("missing defaults.%s", cls)
		}
		config, err := requireMapping(cfgV, "defaults."+cls)
		if err != nil {
			return err
		}
		if err := unknownFields(config, budgetFieldsSet, "defaults."+cls); err != nil {
			return err
		}
		for _, field := range []string{"max_ns_regression_percent", "max_bytes_regression_percent"} {
			fv, _ := config.get(field)
			if _, err := requireNumber(fv, "defaults."+cls+"."+field); err != nil {
				return err
			}
		}
		av, _ := config.get("allow_alloc_increase")
		if err := validateAllocPolicy(av, "defaults."+cls+".allow_alloc_increase"); err != nil {
			return err
		}
	}

	benchmarksV, _ := policy.get("benchmarks")
	benchmarks, err := requireMapping(benchmarksV, "benchmarks")
	if err != nil {
		return err
	}
	if len(benchmarks.keys) == 0 {
		return perr("benchmarks must not be empty")
	}
	for _, name := range benchmarks.keys {
		benchPath := "benchmarks." + name
		config, err := requireMapping(benchmarks.vals[name], benchPath)
		if err != nil {
			return err
		}
		if err := unknownFields(config, benchFieldsSet, benchPath); err != nil {
			return err
		}
		for _, field := range []string{"package", "class", "gate", "harness_files"} {
			if _, ok := config.get(field); !ok {
				return perr("missing %s.%s", benchPath, field)
			}
		}
		pkgV, _ := config.get("package")
		if _, ok := pkgV.(string); !ok {
			return perr("%s.package must be a string", benchPath)
		}
		clsV, _ := config.get("class")
		clsStr, _ := clsV.(string)
		if !classesSet[clsStr] {
			return perr("%s.class must be one of %s", benchPath, pyListRepr(sortedKeys(classesSet)))
		}
		gateV, _ := config.get("gate")
		gateStr, _ := gateV.(string)
		if !gatesSet[gateStr] {
			return perr("%s.gate must be one of %s", benchPath, pyListRepr(sortedKeys(gatesSet)))
		}
		harnessV, _ := config.get("harness_files")
		harness, ok := harnessV.([]any)
		if !ok || len(harness) == 0 {
			return perr("%s.harness_files must be a non-empty list", benchPath)
		}
		seenHarness := make(map[string]bool, len(harness))
		for _, raw := range harness {
			path, ok := raw.(string)
			if !ok || path == "" {
				return perr("%s.harness_files must contain non-empty strings", benchPath)
			}
			if seenHarness[path] {
				return perr("%s.harness_files must not contain duplicates", benchPath)
			}
			seenHarness[path] = true
		}
		for _, field := range []string{"max_ns_regression_percent", "max_bytes_regression_percent"} {
			if fv, ok := config.get(field); ok {
				if _, err := requireNumber(fv, benchPath+"."+field); err != nil {
					return err
				}
			}
		}
		if av, ok := config.get("allow_alloc_increase"); ok {
			if err := validateAllocPolicy(av, benchPath+".allow_alloc_increase"); err != nil {
				return err
			}
		}
	}

	settingsV, _ := policy.get("settings")
	settings, err := requireMapping(settingsV, "settings")
	if err != nil {
		return err
	}
	if err := unknownFields(settings, settingsFieldsSet, "settings"); err != nil {
		return err
	}
	for _, field := range settingsFieldsList {
		if _, ok := settings.get(field); !ok {
			return perr("missing settings.%s", field)
		}
	}
	modeV, _ := settings.get("mode")
	modeStr, _ := modeV.(string)
	if modeStr != "warn" && modeStr != "fail" {
		return perr("settings.mode must be warn or fail")
	}
	if _, err := requireNumber(mustGet(settings, "benchstat_alpha"), "settings.benchstat_alpha"); err != nil {
		return err
	}
	minCount, err := requireNumber(mustGet(settings, "min_count"), "settings.min_count")
	if err != nil {
		return err
	}
	if minCount < 1 {
		return perr("settings.min_count must be at least 1")
	}
	noiseFloor, err := requireNumber(mustGet(settings, "noise_floor_ns"), "settings.noise_floor_ns")
	if err != nil {
		return err
	}
	if noiseFloor < 0 {
		return perr("settings.noise_floor_ns must be non-negative")
	}
	return nil
}

func harnessFilesForSelection(selected selection) []string {
	seen := make(map[string]bool)
	for _, entry := range selected {
		raw, _ := entry.config.get("harness_files")
		for _, value := range raw.([]any) {
			path, _ := value.(string)
			seen[path] = true
		}
	}
	files := make([]string, 0, len(seen))
	for path := range seen {
		files = append(files, path)
	}
	slices.Sort(files)
	return files
}

func selectedBenchmarks(policy *omap, gate string) (selection, error) {
	benchmarks := mapOf(policy, "benchmarks")
	var sel selection
	for _, name := range benchmarks.keys {
		cfg, _ := benchmarks.vals[name].(*omap)
		if gate == "" {
			sel = append(sel, benchEntry{name, cfg})
			continue
		}
		g, _ := mustGet(cfg, "gate").(string)
		if g == gate {
			sel = append(sel, benchEntry{name, cfg})
		}
	}
	if gate != "" && len(sel) == 0 {
		return nil, perr("no benchmarks selected for gate=%s", gate)
	}
	return sel, nil
}

func mapOf(m *omap, key string) *omap {
	v, _ := m.get(key)
	mm, _ := v.(*omap)
	return mm
}

func settingsOf(policy *omap) *omap { return mapOf(policy, "settings") }
func defaultsOf(policy *omap) *omap { return mapOf(policy, "defaults") }
func settingsMode(policy *omap) string {
	v, _ := settingsOf(policy).get("mode")
	s, _ := v.(string)
	return s
}
func policyMinCount(policy *omap) int {
	v, _ := settingsOf(policy).get("min_count")
	return toInt(v)
}

func mustGet(m *omap, k string) any { v, _ := m.get(k); return v }

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func equalsInt1(v any) bool {
	switch n := v.(type) {
	case int:
		return n == 1
	case int64:
		return n == 1
	case uint64:
		return n == 1
	case float64:
		return n == 1
	}
	return false
}

func stringEquals(v any, s string) bool {
	sv, ok := v.(string)
	return ok && sv == s
}

func pyValStr(v any, present bool) string {
	if !present || v == nil {
		return "None"
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return "None"
}

func pyListRepr(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = "'" + s + "'"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
