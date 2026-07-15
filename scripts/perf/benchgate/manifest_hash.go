package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var (
	legacySelectionHashFields = [...]string{
		"package", "class", "gate",
		"max_ns_regression_percent", "max_bytes_regression_percent", "allow_alloc_increase",
	}
	absoluteSelectionHashFields = [...]string{"max_ns_per_op", "max_bytes_per_op", "max_allocs_per_op"}
)

func canonicalHash(write func(*bytes.Buffer)) string {
	var b bytes.Buffer
	write(&b)
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:])
}

func hashString(b *bytes.Buffer, value string) {
	_ = binary.Write(b, binary.BigEndian, uint64(len(value)))
	_, _ = b.WriteString(value)
}

func hashesForEvidence(policyPath, repoRoot string, selected selection) (string, string, string, []string, error) {
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return "", "", "", nil, perr("cannot read policy bytes: %v", err)
	}
	policySum := sha256.Sum256(policyBytes)
	harnessFiles := harnessFilesForSelection(selected)
	for _, path := range harnessFiles {
		if filepath.IsAbs(path) || hasDotDot(path) || filepath.Clean(path) != path {
			return "", "", "", nil, perr("invalid harness file path: %s", path)
		}
		full := filepath.Join(repoRoot, path)
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", "", nil, perr("harness file must be a regular non-symlink file: %s", path)
		}
		cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
		cmd.Dir = repoRoot
		if err := cmd.Run(); err != nil {
			return "", "", "", nil, perr("harness file must be tracked: %s", path)
		}
	}
	harnessHash := canonicalHash(func(b *bytes.Buffer) {
		for _, path := range harnessFiles {
			data, _ := os.ReadFile(filepath.Join(repoRoot, path))
			hashString(b, path)
			_ = binary.Write(b, binary.BigEndian, uint64(len(data)))
			_, _ = b.Write(data)
		}
	})
	ordered := append(selection(nil), selected...)
	slices.SortFunc(ordered, func(a, b benchEntry) int { return strings.Compare(a.name, b.name) })
	hashFields := selectionHashFields(ordered)
	selectionHash := canonicalHash(func(b *bytes.Buffer) {
		for _, entry := range ordered {
			hashString(b, entry.name)
			for _, field := range hashFields {
				value, _ := entry.config.get(field)
				hashString(b, fmt.Sprint(value))
			}
		}
		for _, path := range harnessFiles {
			hashString(b, path)
		}
	})
	return hex.EncodeToString(policySum[:]), selectionHash, harnessHash, harnessFiles, nil
}

func selectionHashFields(selected selection) []string {
	for _, entry := range selected {
		for _, field := range absoluteSelectionHashFields {
			if _, ok := entry.config.get(field); ok {
				fields := slices.Clone(legacySelectionHashFields[:])
				return append(fields, absoluteSelectionHashFields[:]...)
			}
		}
	}
	return legacySelectionHashFields[:]
}

func evidenceContext(policyPath, repoRoot, repoName string, selected selection, args *cliArgs) (EvidenceIdentity, error) {
	if args.gate == "" || args.gateID == "" {
		return EvidenceIdentity{}, perr("--gate and --gate-id are required for v2 evidence")
	}
	policyHash, selectionHash, harnessHash, _, err := hashesForEvidence(policyPath, repoRoot, selected)
	if err != nil {
		return EvidenceIdentity{}, err
	}
	names := make([]string, len(selected))
	for i, entry := range selected {
		names[i] = entry.name
	}
	slices.Sort(names)
	return EvidenceIdentity{Contract: evidenceContract, Repository: repoName, GateID: args.gateID, SelectionGate: args.gate, PolicySHA256: policyHash, SelectionSHA256: selectionHash, HarnessSHA256: harnessHash, Benchmarks: names}, nil
}
