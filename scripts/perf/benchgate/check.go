package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func checkResults(policy *omap, selected selection, args *cliArgs) (int, error) {
	baselinePath := args.baseline
	if hasDotDot(filepath.Clean(pyPathStr(baselinePath))) {
		return 0, perr("baseline path must not contain '..' traversal: %s", pyPathStr(baselinePath))
	}
	candidatePath := args.candidate

	candidate, e := parseResults(candidatePath)
	if e != nil {
		return 0, e
	}
	if len(candidate.Files) == 0 {
		fmt.Fprintf(os.Stderr, "error: candidate path has no result files: %s\n", candidatePath)
		return 2, nil
	}

	missingCand := checkMissing(candidate, selected)
	if len(missingCand) > 0 {
		for _, name := range missingCand {
			fmt.Fprintf(os.Stderr, "error: missing candidate benchmark: %s\n", name)
		}
		return 2, nil
	}

	if len(resultFiles(baselinePath)) == 0 {
		if candidate.Race {
			fmt.Fprintln(os.Stderr, "error: refusing to create baseline from race benchmark results")
			return 2, nil
		}
		if !printRequiredMetricDiagnostics(policy, selected, nil, candidate) {
			return 2, nil
		}
		if e := requireAutoBaselineQuality(policy, candidate, args.allowSmokeBaseline); e != nil {
			return 0, e
		}
		if e := copyCandidateToBaseline(candidatePath, baselinePath); e != nil {
			return 0, e
		}
		fmt.Printf("baseline created: %s\n", pyPathStr(baselinePath))
		return 0, nil
	}

	baseline, e := parseResults(baselinePath)
	if e != nil {
		return 0, e
	}
	if candidate.Race || baseline.Race {
		fmt.Println("skip: race build result detected; benchmark budget comparison skipped")
		return 0, nil
	}

	missingBase := checkMissing(baseline, selected)
	if len(missingBase) > 0 {
		for _, name := range missingBase {
			fmt.Fprintf(os.Stderr, "error: missing baseline benchmark: %s\n", name)
		}
		return 2, nil
	}

	if status, has := checkExistingBaselineQuality(policy, baseline); has {
		return status, nil
	}

	if !printRequiredMetricDiagnostics(policy, selected, baseline, candidate) {
		return 2, nil
	}

	issues := compareResults(policy, selected, baseline, candidate)
	if len(issues) == 0 {
		gate := ""
		if args.gate != "" {
			gate = ", gate=" + args.gate
		}
		fmt.Printf("ok: benchmark budget comparison passed (benchmarks=%d, mode=%s%s)\n",
			len(selected), settingsMode(policy), gate)
		return 0, nil
	}

	printIssues(issues)
	mode := settingsMode(policy)
	if mode == "warn" {
		fmt.Println("warn mode: violations reported; exiting 0")
		return 0, nil
	}
	failEligible := false
	for _, is := range issues {
		if is.Level == "violation" && (is.BenchClass == "critical" || is.BenchClass == "hotpath") {
			failEligible = true
			break
		}
	}
	if failEligible {
		fmt.Println("fail mode: critical/hotpath violations found")
		return 1, nil
	}
	fmt.Println("fail mode: no critical/hotpath violations; exiting 0")
	return 0, nil
}

func requireAutoBaselineQuality(policy *omap, cand *Results, allowSmoke bool) error {
	if allowSmoke {
		return nil
	}
	minCount := policyMinCount(policy)
	if cand.Count == nil {
		return perr("refusing to create baseline from benchmark results without count metadata")
	}
	if *cand.Count < minCount {
		return perr("refusing to create baseline from smoke benchmark results: count=%d < min_count=%d",
			*cand.Count, minCount)
	}
	if cand.Benchtime == nil {
		return perr("refusing to create baseline from benchmark results without benchtime metadata")
	}
	if *cand.Benchtime != DefaultBenchtime {
		return perr("refusing to create baseline from smoke benchmark results: benchtime=%s differs from required %s",
			*cand.Benchtime, DefaultBenchtime)
	}
	return nil
}

func checkExistingBaselineQuality(policy *omap, baseline *Results) (int, bool) {
	minCount := policyMinCount(policy)
	if baseline.Count == nil || *baseline.Count >= minCount {
		return 0, false
	}
	source := ""
	if baseline.CountFile != "" {
		source = " file=" + baseline.CountFile
	}
	message := fmt.Sprintf("existing baseline is smoke/junk baseline: count=%d < min_count=%d%s",
		*baseline.Count, minCount, source)
	if settingsMode(policy) == "fail" {
		fmt.Fprintf(os.Stderr, "error: %s\n", message)
		return 2, true
	}
	fmt.Printf("warning: %s\n", message)
	return 0, false
}

func copyCandidateToBaseline(candidate, baseline string) error {
	fi, err := os.Stat(candidate)
	if err != nil {
		return perr("candidate path does not exist: %s", candidate)
	}
	if !fi.IsDir() {
		if err := os.MkdirAll(baseline, 0o755); err != nil {
			return perr("cannot create %s: %v", baseline, err)
		}
		return copyFile(candidate, filepath.Join(baseline, filepath.Base(candidate)))
	}
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		return perr("cannot create %s: %v", baseline, err)
	}
	for _, source := range resultFiles(candidate) {
		rel, rerr := filepath.Rel(candidate, source)
		if rerr != nil {
			return perr("cannot relativize %s: %v", source, rerr)
		}
		target := filepath.Join(baseline, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return perr("cannot create %s: %v", filepath.Dir(target), err)
		}
		if err := copyFile(source, target); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return perr("cannot read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return perr("cannot write %s: %v", dst, err)
	}
	return nil
}
