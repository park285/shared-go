package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const evidenceContract = "benchgate-evidence-v2"

type EvidenceIdentity struct {
	Contract        string   `json:"contract"`
	Repository      string   `json:"repository"`
	GateID          string   `json:"gate_id"`
	SelectionGate   string   `json:"selection_gate"`
	PolicySHA256    string   `json:"policy_sha256"`
	SelectionSHA256 string   `json:"selection_sha256"`
	HarnessSHA256   string   `json:"harness_sha256"`
	Benchmarks      []string `json:"benchmarks"`
}

type EvidenceEnvironment struct {
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	CPUModel  string `json:"cpu_model"`
}

type EvidenceCommand struct {
	Package string   `json:"package"`
	WorkDir string   `json:"workdir"`
	Argv    []string `json:"argv"`
}

type EvidenceCollection struct {
	Count     int               `json:"count"`
	Benchtime string            `json:"benchtime"`
	Benchmem  bool              `json:"benchmem"`
	Race      bool              `json:"race"`
	Commands  []EvidenceCommand `json:"commands"`
}

type CandidateManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	EvidenceRole  string              `json:"evidence_role"`
	GitSHA        string              `json:"git_sha"`
	GitDirty      bool                `json:"git_dirty"`
	CreatedAt     string              `json:"created_at"`
	Identity      EvidenceIdentity    `json:"identity"`
	Environment   EvidenceEnvironment `json:"environment"`
	Collection    EvidenceCollection  `json:"collection"`
	Files         map[string]string   `json:"files"`
}

type BaselineManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	EvidenceRole  string              `json:"evidence_role"`
	GitSHA        string              `json:"git_sha"`
	ApprovedSHA   string              `json:"approved_sha"`
	GitDirty      bool                `json:"git_dirty"`
	CreatedAt     string              `json:"created_at"`
	Identity      EvidenceIdentity    `json:"identity"`
	Environment   EvidenceEnvironment `json:"environment"`
	Collection    EvidenceCollection  `json:"collection"`
	Files         map[string]string   `json:"files"`
}

func manifestPath(root, role string) string { return filepath.Join(root, role+"-manifest.json") }

func validHex(value string, length int) bool {
	_, err := hex.DecodeString(value)
	return len(value) == length && err == nil
}

func validateIdentity(id EvidenceIdentity) error {
	if id.Contract != evidenceContract || id.Repository == "" || id.GateID == "" || !gatesSet[id.SelectionGate] {
		return perr("invalid manifest identity")
	}
	for _, digest := range []string{id.PolicySHA256, id.SelectionSHA256, id.HarnessSHA256} {
		if !validHex(digest, 64) {
			return perr("invalid manifest identity digest")
		}
	}
	if len(id.Benchmarks) == 0 || !slices.IsSorted(id.Benchmarks) {
		return perr("manifest benchmarks must be non-empty and sorted")
	}
	for i, name := range id.Benchmarks {
		if name == "" || i > 0 && name == id.Benchmarks[i-1] {
			return perr("manifest benchmarks must be unique and non-empty")
		}
	}
	return nil
}

func validateCollection(collection EvidenceCollection) error {
	if collection.Count < 1 || collection.Benchtime == "" || !collection.Benchmem || len(collection.Commands) == 0 {
		return perr("invalid manifest collection")
	}
	last := ""
	for _, command := range collection.Commands {
		if command.Package == "" || command.WorkDir == "" || filepath.IsAbs(command.WorkDir) || hasDotDot(command.WorkDir) || len(command.Argv) == 0 {
			return perr("invalid manifest command")
		}
		key := command.Package + "\x00" + command.WorkDir
		if key <= last {
			return perr("manifest commands must be sorted and unique")
		}
		last = key
		for _, part := range command.Argv {
			if part == "" {
				return perr("manifest command argv must not contain empty values")
			}
		}
	}
	return nil
}

func validateFiles(root, role string, files map[string]string) error {
	if len(files) == 0 {
		return perr("manifest files must not be empty")
	}
	if fi, err := os.Lstat(root); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return perr("manifest root must be a non-symlink directory")
	}
	listed := make(map[string]bool, len(files))
	for path, checksum := range files {
		if !canonicalManifestFilePath(path) || !validHex(checksum, 64) {
			return perr("invalid manifest file entry")
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		fi, err := os.Lstat(full)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
			return perr("manifest file is not a regular file: %s", path)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return perr("cannot read manifest file %s: %v", path, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != checksum {
			return perr("manifest checksum mismatch: %s", path)
		}
		listed[path] = true
	}
	expectedManifest := role + "-manifest.json"
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return perr("manifest directory contains symlink: %s", rel)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return perr("manifest directory contains non-regular file: %s", rel)
		}
		if rel == expectedManifest {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !canonicalManifestFilePath(rel) {
			return perr("manifest directory contains non-canonical file path: %s", rel)
		}
		if !listed[rel] {
			return perr("manifest directory contains unlisted file: %s", rel)
		}
		return nil
	})
	if err != nil {
		var policyErr *policyError
		if errors.As(err, &policyErr) {
			return policyErr
		}
		return perr("cannot walk manifest directory: %v", err)
	}
	return nil
}

func canonicalManifestFilePath(value string) bool {
	return value != "" && !strings.Contains(value, `\`) && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../")
}

func validateCandidateManifest(root string) (*CandidateManifest, error) {
	data, err := os.ReadFile(manifestPath(root, "candidate"))
	if err != nil {
		return nil, perr("cannot read candidate manifest: %v", err)
	}
	var manifest CandidateManifest
	if err := strictJSON(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != 2 || manifest.EvidenceRole != "candidate" || !validHex(manifest.GitSHA, 40) {
		return nil, perr("invalid candidate manifest role or git SHA")
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
		return nil, perr("invalid candidate manifest timestamp")
	}
	if err := validateIdentity(manifest.Identity); err != nil {
		return nil, err
	}
	if manifest.Environment.GoVersion == "" || manifest.Environment.GOOS == "" || manifest.Environment.GOARCH == "" || manifest.Environment.CPUModel == "" {
		return nil, perr("invalid candidate manifest environment")
	}
	if err := validateCollection(manifest.Collection); err != nil {
		return nil, err
	}
	if err := validateFiles(root, "candidate", manifest.Files); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validateBaselineManifest(root string) (*BaselineManifest, error) {
	data, err := os.ReadFile(manifestPath(root, "baseline"))
	if err != nil {
		return nil, perr("cannot read baseline manifest: %v", err)
	}
	var manifest BaselineManifest
	if err := strictJSON(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != 2 || manifest.EvidenceRole != "baseline" || !validHex(manifest.GitSHA, 40) || manifest.ApprovedSHA != manifest.GitSHA || manifest.GitDirty {
		return nil, perr("invalid baseline manifest role or provenance")
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
		return nil, perr("invalid baseline manifest timestamp")
	}
	if err := validateIdentity(manifest.Identity); err != nil {
		return nil, err
	}
	if manifest.Environment.GoVersion == "" || manifest.Environment.GOOS == "" || manifest.Environment.GOARCH == "" || manifest.Environment.CPUModel == "" {
		return nil, perr("invalid baseline manifest environment")
	}
	if err := validateCollection(manifest.Collection); err != nil {
		return nil, err
	}
	if err := validateFiles(root, "baseline", manifest.Files); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func gitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitState(root string) (string, bool, error) {
	sha, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil || !validHex(sha, 40) {
		return "", false, perr("cannot determine clean Git SHA")
	}
	status, err := gitOutput(root, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return "", false, perr("cannot determine Git status")
	}
	return sha, status != "", nil
}

func cpuModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if before, after, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(before) == "model name" && strings.TrimSpace(after) != "" {
				return strings.TrimSpace(after)
			}
		}
	}
	return runtime.GOARCH
}
