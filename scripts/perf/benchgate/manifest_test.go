package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingResultSink struct {
	*os.File
	fail string
}

func (sink *failingResultSink) Write(data []byte) (int, error) {
	if sink.fail == "write" {
		return 0, errors.New("injected write failure")
	}
	return sink.File.Write(data)
}

func (sink *failingResultSink) Sync() error {
	if sink.fail == "sync" {
		return errors.New("injected sync failure")
	}
	return sink.File.Sync()
}

func (sink *failingResultSink) Close() error {
	err := sink.File.Close()
	if sink.fail == "close" {
		return errors.New("injected close failure")
	}
	return err
}

func testIdentity() EvidenceIdentity {
	return EvidenceIdentity{Contract: evidenceContract, Repository: "fixture", GateID: "fixture-gate", SelectionGate: "pr", PolicySHA256: strings.Repeat("1", 64), SelectionSHA256: strings.Repeat("2", 64), HarnessSHA256: strings.Repeat("3", 64), Benchmarks: []string{"BenchmarkTarget"}}
}

func writeManifestResult(t *testing.T, root, role string) map[string]string {
	t.Helper()
	path := filepath.Join(root, "go-bench", "fixture.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("# package: ./fixture\nBenchmarkTarget-1 1 100 ns/op 8 B/op 1 allocs/op\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return map[string]string{"go-bench/fixture.txt": hex.EncodeToString(sum[:])}
}

func writeCandidate(t *testing.T, root string, race bool) *CandidateManifest {
	t.Helper()
	files := writeManifestResult(t, root, "candidate")
	manifest := &CandidateManifest{SchemaVersion: 2, EvidenceRole: "candidate", GitSHA: strings.Repeat("a", 40), CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: testIdentity(), Environment: EvidenceEnvironment{GoVersion: "go1.test", GOOS: "linux", GOARCH: "amd64", CPUModel: "fixture"}, Collection: EvidenceCollection{Count: 2, Benchtime: "100ms", Benchmem: true, Race: race, Commands: []EvidenceCommand{{Package: "./fixture", WorkDir: ".", Argv: []string{"go", "test"}}}}, Files: files}
	if err := writeJSONAtomic(manifestPath(root, "candidate"), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeBaseline(t *testing.T, root string, candidate *CandidateManifest, race bool) *BaselineManifest {
	t.Helper()
	files := writeManifestResult(t, root, "baseline")
	manifest := &BaselineManifest{SchemaVersion: 2, EvidenceRole: "baseline", GitSHA: candidate.GitSHA, ApprovedSHA: candidate.GitSHA, CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: candidate.Identity, Environment: candidate.Environment, Collection: candidate.Collection, Files: files}
	manifest.Collection.Race = race
	if err := writeJSONAtomic(manifestPath(root, "baseline"), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestStrictManifestRejectsAmbiguousAndUnsafeEvidence(t *testing.T) {
	t.Run("valid candidate", func(t *testing.T) {
		root := t.TempDir()
		writeCandidate(t, root, false)
		if _, err := validateCandidateManifest(root); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{"duplicate key", func(t *testing.T, root string) {
			path := manifestPath(root, "candidate")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), `"schema_version": 2,`, `"schema_version": 2,"schema_version": 2,`, 1))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"unknown field", func(t *testing.T, root string) {
			path := manifestPath(root, "candidate")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), "{", `{"unknown":true,`, 1))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing field", func(t *testing.T, root string) {
			path := manifestPath(root, "candidate")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), `"git_dirty": false,`, "", 1))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra file", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, root string) {
			if err := os.Symlink(filepath.Join(root, "go-bench", "fixture.txt"), filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
		}},
		{"checksum", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "go-bench", "fixture.txt"), []byte("changed"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCandidate(t, root, false)
			tc.mutate(t, root)
			if _, err := validateCandidateManifest(root); err == nil {
				t.Fatal("validation unexpectedly succeeded")
			}
		})
	}
}

func TestCompatibleV2RejectsRaceAndMismatches(t *testing.T) {
	candidateRoot, baselineRoot := t.TempDir(), t.TempDir()
	candidate := writeCandidate(t, candidateRoot, false)
	baseline := writeBaseline(t, baselineRoot, candidate, false)
	if err := compatibleV2(candidate, baseline); err != nil {
		t.Fatal(err)
	}
	baseline.Collection.Race = true
	if err := compatibleV2(candidate, baseline); err == nil {
		t.Fatal("race evidence unexpectedly accepted")
	}
	baseline.Collection.Race = false
	baseline.Identity.GateID = "other"
	if err := compatibleV2(candidate, baseline); err == nil {
		t.Fatal("gate-id mismatch unexpectedly accepted")
	}
}

func TestValidateFilesRejectsNonCanonicalAliases(t *testing.T) {
	root := t.TempDir()
	files := writeManifestResult(t, root, "candidate")
	checksum := files["go-bench/fixture.txt"]
	for _, alias := range []string{"go-bench//fixture.txt", "go-bench/./fixture.txt", `go-bench\fixture.txt`} {
		t.Run(alias, func(t *testing.T) {
			if err := validateFiles(root, "candidate", map[string]string{alias: checksum}); err == nil {
				t.Fatal("non-canonical alias unexpectedly accepted")
			}
		})
	}
	if err := validateFiles(root, "candidate", map[string]string{"go-bench/fixture.txt": checksum, "go-bench//fixture.txt": checksum}); err == nil {
		t.Fatal("duplicate logical result alias unexpectedly accepted")
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func setupCollectionFixture(t *testing.T) (string, *omap, selection, *cliArgs) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.26.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	benchmark := "package fixture\n\nimport \"testing\"\n\nfunc BenchmarkTarget(b *testing.B) {\n\tfor i := 0; i < b.N; i++ {\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(root, "fixture", "bench_test.go"), []byte(benchmark), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "schemaVersion: 1\nrepo: fixture\ndefaults:\n  critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n  hotpath: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n  build_path: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: warn }\n  non_critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: warn }\nbenchmarks:\n  BenchmarkTarget: { package: ./fixture, class: critical, gate: pr, harness_files: [fixture/bench_test.go] }\nsettings: { mode: fail, benchstat_alpha: 0.05, min_count: 1, noise_floor_ns: 0 }\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("artifacts/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "benchgate@example.invalid")
	runGit(t, root, "config", "user.name", "benchgate")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "fixture")
	policy, loadErr := loadPolicy(policyPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if err := validatePolicy(policy, "fixture"); err != nil {
		t.Fatal(err)
	}
	selected, err := selectedBenchmarks(policy, "pr")
	if err != nil {
		t.Fatal(err)
	}
	return root, policy, selected, &cliArgs{policy: policyPath, candidate: "artifacts/perf/candidate", gate: "pr", gateID: "fixture-gate", count: ptr(1), benchtime: "1ms"}
}

func ptr(value int) *int { return &value }

func TestCollectDoesNotPublishManifestAfterSinkFailure(t *testing.T) {
	for _, fail := range []string{"write", "sync", "close"} {
		t.Run(fail, func(t *testing.T) {
			root, policy, selected, args := setupCollectionFixture(t)
			originalOpen := openResultSink
			openResultSink = func(path string) (resultSink, error) {
				file, err := os.Create(path)
				if err != nil {
					return nil, err
				}
				return &failingResultSink{File: file, fail: fail}, nil
			}
			defer func() { openResultSink = originalOpen }()
			workingDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Chdir(workingDir) }()
			if _, err := collectResults(policy, selected, args, root, "fixture"); err == nil {
				t.Fatal("collection unexpectedly succeeded")
			}
			if _, err := os.Stat(filepath.Join(root, args.candidate, "candidate-manifest.json")); !os.IsNotExist(err) {
				t.Fatal("sink failure published a candidate manifest")
			}
		})
	}
}

func TestBootstrapBaselineWritesAbsentTargetOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.26.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture", "bench_test.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "schemaVersion: 1\nrepo: fixture\ndefaults:\n  critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n  hotpath: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: false }\n  build_path: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: warn }\n  non_critical: { max_ns_regression_percent: 1, max_bytes_regression_percent: 1, allow_alloc_increase: warn }\nbenchmarks:\n  BenchmarkTarget: { package: ./fixture, class: critical, gate: pr, harness_files: [fixture/bench_test.go] }\nsettings: { mode: fail, benchstat_alpha: 0.05, min_count: 2, noise_floor_ns: 0 }\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("artifacts/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "benchgate@example.invalid")
	runGit(t, root, "config", "user.name", "benchgate")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-qm", "fixture")
	policy, loadErr := loadPolicy(policyPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if err := validatePolicy(policy, "fixture"); err != nil {
		t.Fatal(err)
	}
	selected, err := selectedBenchmarks(policy, "pr")
	if err != nil {
		t.Fatal(err)
	}
	args := &cliArgs{policy: policyPath, candidate: filepath.Join(root, "artifacts/perf/candidate"), baseline: filepath.Join(root, "artifacts/perf/baseline"), gate: "pr", gateID: "fixture-gate"}
	identity, err := evidenceContext(policyPath, root, "fixture", selected, args)
	if err != nil {
		t.Fatal(err)
	}
	sha, dirty, err := gitState(root)
	if err != nil || dirty {
		t.Fatalf("unexpected git state: %s %v %v", sha, dirty, err)
	}
	candidateRoot := args.candidate
	files := writeManifestResult(t, candidateRoot, "candidate")
	fixtureFile := filepath.Join(root, "fixture", "bench_test.go")
	if err := os.WriteFile(fixtureFile, []byte("package fixture\nvar collectedWhileDirty = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, collectedDirty, err := gitState(root)
	if err != nil || !collectedDirty {
		t.Fatalf("expected dirty collection state: dirty=%v err=%v", collectedDirty, err)
	}
	candidate := CandidateManifest{SchemaVersion: 2, EvidenceRole: "candidate", GitSHA: sha, GitDirty: collectedDirty, CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: identity, Environment: EvidenceEnvironment{GoVersion: "go1.test", GOOS: "linux", GOARCH: "amd64", CPUModel: "fixture"}, Collection: EvidenceCollection{Count: 2, Benchtime: "100ms", Benchmem: true, Commands: []EvidenceCommand{{Package: "./fixture", WorkDir: ".", Argv: []string{"go", "test"}}}}, Files: files}
	if err := writeJSONAtomic(manifestPath(candidateRoot, "candidate"), candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixtureFile, []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cleanedDirty, err := gitState(root)
	if err != nil || cleanedDirty {
		t.Fatalf("expected clean bootstrap state: dirty=%v err=%v", cleanedDirty, err)
	}
	args.approvedSHA = sha
	if _, err := bootstrapBaseline(policy, selected, args, root, "fixture"); err == nil {
		t.Fatal("bootstrap accepted candidate collected from a dirty worktree")
	}
	if _, err := os.Stat(args.baseline); !os.IsNotExist(err) {
		t.Fatal("dirty candidate created a baseline")
	}
	candidate.GitDirty = false
	if err := writeJSONAtomic(manifestPath(candidateRoot, "candidate"), candidate); err != nil {
		t.Fatal(err)
	}
	if code, err := bootstrapBaseline(policy, selected, args, root, "fixture"); err != nil || code != 0 {
		t.Fatalf("bootstrap: code=%d err=%v", code, err)
	}
	if _, err := validateBaselineManifest(args.baseline); err != nil {
		t.Fatal(err)
	}
	if code, err := checkV2Results(policy, selected, args, root, "fixture"); err != nil || code != 0 {
		t.Fatalf("check-v2: code=%d err=%v", code, err)
	}
	if _, err := bootstrapBaseline(policy, selected, args, root, "fixture"); err == nil {
		t.Fatal("bootstrap replaced existing baseline")
	}
	if err := os.RemoveAll(args.baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := checkV2Results(policy, selected, args, root, "fixture"); err == nil {
		t.Fatal("check-v2 accepted a missing baseline")
	}
	if _, err := os.Stat(args.baseline); !os.IsNotExist(err) {
		t.Fatal("check-v2 wrote a missing baseline")
	}
}
