package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var perfArtifactRoot = "artifacts/perf"

type resultSink interface {
	io.Writer
	Sync() error
	Close() error
}

var openResultSink = func(path string) (resultSink, error) {
	return os.Create(path)
}

func ensureSafePerfCandidatePath(arg, repoRoot string) (string, error) {
	display := pyPathStr(arg)
	if filepath.IsAbs(arg) {
		return "", perr("candidate path must be a relative path under artifacts/perf/: %s", display)
	}
	normalized := filepath.Clean(display)
	if normalized == "." || hasDotDot(normalized) {
		return "", perr("candidate path must be a relative path under artifacts/perf/: %s", display)
	}
	if !isUnder(perfArtifactRoot, normalized) {
		return "", perr("candidate path must be a relative path under artifacts/perf/: %s", display)
	}
	current := repoRoot
	for part := range strings.SplitSeq(normalized, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if fi, err := os.Lstat(current); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			sym := current
			if rel, rerr := filepath.Rel(repoRoot, current); rerr == nil {
				sym = rel
			}
			return "", perr("candidate path must not traverse symlink under artifacts/perf/: %s", sym)
		}
	}
	return normalized, nil
}

func resolvePackage(repoRoot, pkg string) (string, string, error) {
	if !strings.HasPrefix(pkg, "./") {
		return repoRoot, pkg, nil
	}
	pkgDir := filepath.Join(repoRoot, pkg[2:])
	if !isUnder(resolveSymlinks(repoRoot), resolveSymlinks(pkgDir)) {
		return "", "", perr("benchmark package path must stay under repo root: %s", pkg)
	}
	if !fileExists(pkgDir) {
		return repoRoot, pkg, nil
	}
	moduleDir := ""
	if fileExists(filepath.Join(repoRoot, "go.mod")) {
		moduleDir = repoRoot
	}
	current := pkgDir
	for {
		if fileExists(filepath.Join(current, "go.mod")) {
			moduleDir = current
			break
		}
		if current == repoRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if moduleDir == "" || moduleDir == repoRoot {
		return repoRoot, pkg, nil
	}
	rel, _ := filepath.Rel(moduleDir, pkgDir)
	if rel == "." {
		return moduleDir, ".", nil
	}
	return moduleDir, "./" + filepath.ToSlash(rel), nil
}

func benchRegex(names []string) string {
	escaped := make([]string, len(names))
	for i, n := range names {
		escaped[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(escaped, "|") + ")$"
}

func exitCode(err error) int {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode()
	}
	return 1
}

func effectiveCollectionRace(repoRoot string) (bool, error) {
	cmd := exec.Command("go", "env", "GOFLAGS")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return false, perr("cannot determine effective GOFLAGS: %v", err)
	}
	return goFlagsEnableRace(strings.TrimSpace(string(out))), nil
}

func goFlagsEnableRace(flags string) bool {
	for _, flag := range strings.Fields(flags) {
		if flag == "-race" {
			return true
		}
		value, found := strings.CutPrefix(flag, "-race=")
		if !found {
			continue
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil || enabled {
			return true
		}
	}
	return false
}

func collectResults(policy *omap, selected selection, args *cliArgs, repoRoot, repoName string) (int, error) {
	race, err := effectiveCollectionRace(repoRoot)
	if err != nil {
		return 0, err
	}
	if race {
		fmt.Fprintln(os.Stderr, "error: benchmark collection rejects effective race instrumentation")
		return 2, nil
	}
	candidateNorm, e := ensureSafePerfCandidatePath(args.candidate, repoRoot)
	if e != nil {
		return 0, e
	}
	if fi, err := os.Stat(candidateNorm); err == nil {
		if !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "error: candidate path exists and is not a directory: %s\n", candidateNorm)
			return 2, nil
		}
		if err := os.RemoveAll(candidateNorm); err != nil {
			return 0, perr("cannot remove %s: %v", candidateNorm, err)
		}
	}
	outputDir := filepath.Join(candidateNorm, "go-bench")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return 0, perr("cannot create %s: %v", outputDir, err)
	}
	outputFile := filepath.Join(outputDir, repoName+".txt")
	count := policyMinCount(policy)
	if args.count != nil {
		count = *args.count
	}
	benchtime := args.benchtime
	minCount := policyMinCount(policy)
	smokeRun := count < minCount

	byPackage := map[string][]string{}
	for _, en := range selected {
		pkg, _ := mustGet(en.config, "package").(string)
		byPackage[pkg] = append(byPackage[pkg], en.name)
	}
	sortedPkgs := make([]string, 0, len(byPackage))
	for p := range byPackage {
		sortedPkgs = append(sortedPkgs, p)
	}
	slices.Sort(sortedPkgs)

	sink, err := openResultSink(outputFile)
	if err != nil {
		return 0, perr("cannot create %s: %v", outputFile, err)
	}
	sinkClosed := false
	closeSink := func() error {
		if sinkClosed {
			return nil
		}
		sinkClosed = true
		if err := sink.Close(); err != nil {
			return perr("cannot close collected result %s: %v", outputFile, err)
		}
		return nil
	}
	commands := make([]EvidenceCommand, 0, len(sortedPkgs))

	for _, pkg := range sortedPkgs {
		command, out, code, err := runBenchmarkPackage(sink, outputFile, repoRoot, pkg, byPackage[pkg], count, benchtime)
		if err != nil {
			if err := closeSink(); err != nil {
				return 0, err
			}
			return 0, err
		}
		if code != 0 {
			if closeErr := closeSink(); closeErr != nil {
				return 0, closeErr
			}
			_, _ = os.Stderr.Write(out)
			fmt.Fprintf(os.Stderr, "error: benchmark collection failed for package %s\n", pkg)
			return code, nil
		}
		commands = append(commands, command)
	}
	if err := closeSink(); err != nil {
		return 0, err
	}

	results, pe := parseResults(candidateNorm)
	if pe != nil {
		return 0, pe
	}
	missing := checkMissing(results, selected)
	if len(missing) > 0 {
		for _, name := range missing {
			fmt.Fprintf(os.Stderr, "error: missing candidate benchmark: %s\n", name)
		}
		return 2, nil
	}
	identity, err := evidenceContext(args.policy, repoRoot, repoName, selected, args)
	if err != nil {
		return 0, err
	}
	gitSHA, dirty, err := gitState(repoRoot)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(outputFile)
	if err != nil {
		return 0, perr("cannot read collected result: %v", err)
	}
	sum := sha256.Sum256(data)
	manifest := CandidateManifest{
		SchemaVersion: 2, EvidenceRole: "candidate", GitSHA: gitSHA, GitDirty: dirty,
		CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: identity,
		Environment: EvidenceEnvironment{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUModel: cpuModel()},
		Collection:  EvidenceCollection{Count: count, Benchtime: benchtime, Benchmem: true, Race: race, Commands: commands},
		Files:       map[string]string{filepath.ToSlash(filepath.Join("go-bench", repoName+".txt")): hex.EncodeToString(sum[:])},
	}
	if err := writeJSONAtomic(manifestPath(candidateNorm, "candidate"), manifest); err != nil {
		return 0, perr("cannot write candidate manifest: %v", err)
	}
	suffix := ""
	if benchtime != "" {
		suffix = ", benchtime=" + benchtime
	}
	fmt.Printf("collected benchmark results: %s (benchmarks=%d, packages=%d, count=%d%s)\n",
		outputFile, len(selected), len(byPackage), count, suffix)
	if smokeRun {
		fmt.Printf("smoke run: count<min_count (count=%d, min_count=%d)\n", count, minCount)
	}
	return 0, nil
}

func runBenchmarkPackage(sink resultSink, outputFile, repoRoot, pkg string, names []string, count int, benchtime string) (EvidenceCommand, []byte, int, error) {
	cwd, pkgArg, err := resolvePackage(repoRoot, pkg)
	if err != nil {
		return EvidenceCommand{}, nil, 0, err
	}
	sortedNames := append([]string(nil), names...)
	slices.Sort(sortedNames)
	cmdParts := []string{"go", "test", "-run", "^$", "-bench", benchRegex(sortedNames), "-benchmem", fmt.Sprintf("-count=%d", count)}
	if benchtime != "" {
		cmdParts = append(cmdParts, "-benchtime="+benchtime)
	}
	cmdParts = append(cmdParts, pkgArg)
	workDir, err := filepath.Rel(repoRoot, cwd)
	if err != nil {
		return EvidenceCommand{}, nil, 0, perr("cannot derive benchmark work directory: %v", err)
	}
	if workDir == "." {
		workDir = "."
	}
	command := EvidenceCommand{Package: pkg, WorkDir: filepath.ToSlash(workDir), Argv: append([]string(nil), cmdParts...)}
	if _, err := fmt.Fprintf(sink, "# package: %s\n", pkg); err != nil {
		return EvidenceCommand{}, nil, 0, perr("cannot write collected result %s: %v", outputFile, err)
	}

	var buf bytes.Buffer
	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	out := buf.Bytes()
	if _, err := sink.Write(out); err != nil {
		return EvidenceCommand{}, nil, 0, perr("cannot write collected result %s: %v", outputFile, err)
	}
	if len(out) > 0 && out[len(out)-1] != '\n' {
		if _, err := sink.Write([]byte("\n")); err != nil {
			return EvidenceCommand{}, nil, 0, perr("cannot write collected result %s: %v", outputFile, err)
		}
	}
	if err := sink.Sync(); err != nil {
		return EvidenceCommand{}, nil, 0, perr("cannot sync collected result %s: %v", outputFile, err)
	}
	if runErr != nil {
		return EvidenceCommand{}, out, exitCode(runErr), nil
	}
	return command, nil, 0, nil
}
