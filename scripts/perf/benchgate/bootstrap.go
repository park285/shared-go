package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func validateV2Context(manifest *CandidateManifest, policy *omap, selected selection, args *cliArgs, repoRoot, repoName string) error {
	want, err := evidenceContext(args.policy, repoRoot, repoName, selected, args)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(manifest.Identity, want) {
		return perr("candidate manifest identity does not match current policy, harness, selection, repository, gate, or gate-id")
	}
	return nil
}

func compatibleV2(candidate *CandidateManifest, baseline *BaselineManifest) error {
	if candidate.Collection.Race || baseline.Collection.Race {
		return perr("race benchmark evidence is invalid")
	}
	if candidate.SchemaVersion != baseline.SchemaVersion || !reflect.DeepEqual(candidate.Identity, baseline.Identity) || !reflect.DeepEqual(candidate.Environment, baseline.Environment) || !reflect.DeepEqual(candidate.Collection, baseline.Collection) {
		return perr("candidate and baseline manifests are incompatible")
	}
	if len(candidate.Files) != len(baseline.Files) {
		return perr("candidate and baseline result file sets differ")
	}
	for path := range candidate.Files {
		if _, ok := baseline.Files[path]; !ok {
			return perr("candidate and baseline result file sets differ")
		}
	}
	return nil
}

func checkResults(policy *omap, selected selection, args *cliArgs, repoRoot, repoName string) (int, error) {
	candidate, err := validateCandidateManifest(args.candidate)
	if err != nil {
		return 0, err
	}
	if err := validateV2Context(candidate, policy, selected, args, repoRoot, repoName); err != nil {
		return 0, err
	}
	baseline, err := validateBaselineManifest(args.baseline)
	if err != nil {
		return 0, err
	}
	if err := compatibleV2(candidate, baseline); err != nil {
		return 0, err
	}
	candidateResults, err := parseResultsFromManifest(args.candidate, candidate.Files)
	if err != nil {
		return 0, err
	}
	baselineResults, err := parseResultsFromManifest(args.baseline, baseline.Files)
	if err != nil {
		return 0, err
	}
	if missing := checkMissing(candidateResults, selected); len(missing) > 0 {
		for _, name := range missing {
			fmt.Fprintf(os.Stderr, "error: missing candidate benchmark: %s\n", name)
		}
		return 2, nil
	}
	if missing := checkMissing(baselineResults, selected); len(missing) > 0 {
		for _, name := range missing {
			fmt.Fprintf(os.Stderr, "error: missing baseline benchmark: %s\n", name)
		}
		return 2, nil
	}
	if !printRequiredMetricDiagnostics(policy, selected, baselineResults, candidateResults) {
		return 2, nil
	}
	issues := compareResults(policy, selected, baselineResults, candidateResults)
	if len(issues) == 0 {
		fmt.Printf("ok: strict benchmark budget comparison passed (benchmarks=%d, mode=%s, gate=%s, gate-id=%s)\n", len(selected), settingsMode(policy), args.gate, args.gateID)
		return 0, nil
	}
	printIssues(issues)
	if settingsMode(policy) == "warn" {
		fmt.Println("warn mode: violations reported; exiting 0")
		return 0, nil
	}
	for _, issue := range issues {
		if issue.Level == "violation" && (issue.BenchClass == "critical" || issue.BenchClass == "hotpath") {
			fmt.Println("fail mode: critical/hotpath violations found")
			return 1, nil
		}
	}
	fmt.Println("fail mode: no critical/hotpath violations; exiting 0")
	return 0, nil
}

func bootstrapBaseline(policy *omap, selected selection, args *cliArgs, repoRoot, repoName string) (int, error) {
	if strings.TrimSpace(args.baseline) == "" || hasDotDot(args.baseline) {
		return 0, perr("baseline path must not contain '..' traversal: %s", args.baseline)
	}
	candidate, err := validateCandidateManifest(args.candidate)
	if err != nil {
		return 0, err
	}
	if err := validateV2Context(candidate, policy, selected, args, repoRoot, repoName); err != nil {
		return 0, err
	}
	if candidate.GitDirty {
		return 0, perr("bootstrap requires candidate manifest collected from a clean Git worktree")
	}
	if candidate.Collection.Race {
		return 0, perr("race benchmark evidence is invalid")
	}
	head, dirty, err := gitState(repoRoot)
	if err != nil {
		return 0, err
	}
	if dirty || head != candidate.GitSHA || head != args.approvedSHA {
		return 0, perr("bootstrap requires clean HEAD, candidate git_sha, and --approved-sha to match")
	}
	if fi, err := os.Lstat(args.baseline); err == nil || !os.IsNotExist(err) {
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return 0, perr("baseline path must not be a symlink")
		}
		return 0, perr("baseline path already exists: %s", args.baseline)
	}
	parent := filepath.Dir(args.baseline)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return 0, perr("cannot create baseline parent: %v", err)
	}
	for current := parent; current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return 0, perr("baseline path must not traverse symlink: %s", current)
		}
	}
	tmp, err := os.MkdirTemp(parent, ".baseline-")
	if err != nil {
		return 0, perr("cannot create baseline temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for rel := range candidate.Files {
		source := filepath.Join(args.candidate, rel)
		target := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, perr("cannot create baseline result directory: %v", err)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return 0, perr("cannot read candidate result: %v", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return 0, perr("cannot write baseline result: %v", err)
		}
	}
	baseline := BaselineManifest{SchemaVersion: 2, EvidenceRole: "baseline", GitSHA: candidate.GitSHA, ApprovedSHA: args.approvedSHA, GitDirty: false, CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: candidate.Identity, Environment: candidate.Environment, Collection: candidate.Collection, Files: candidate.Files}
	if err := writeJSONAtomic(manifestPath(tmp, "baseline"), baseline); err != nil {
		return 0, perr("cannot write baseline manifest: %v", err)
	}
	if err := os.Rename(tmp, args.baseline); err != nil {
		return 0, perr("cannot atomically install baseline: %v", err)
	}
	if _, err := validateBaselineManifest(args.baseline); err != nil {
		return 0, err
	}
	fmt.Printf("baseline bootstrapped: %s\n", args.baseline)
	return 0, nil
}
