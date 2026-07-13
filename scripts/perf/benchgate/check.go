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
		fmt.Fprintf(os.Stderr, "error: required baseline has no result files: %s\n", baselinePath)
		return 2, nil
	}

	baseline, e := parseResults(baselinePath)
	if e != nil {
		return 0, e
	}
	missingBase := checkMissing(baseline, selected)
	if len(missingBase) > 0 {
		for _, name := range missingBase {
			fmt.Fprintf(os.Stderr, "error: missing baseline benchmark: %s\n", name)
		}
		return 2, nil
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
