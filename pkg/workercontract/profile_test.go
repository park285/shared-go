package workercontract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workercontract"
)

func chatbotIdentity(t *testing.T) workercontract.Identity {
	t.Helper()
	identity, err := workercontract.KnownIdentity("chatbotgo", "bot")
	if err != nil {
		t.Fatalf("KnownIdentity() error = %v", err)
	}
	return identity
}

func validProfilePath() string {
	return filepath.Join("testdata", "stack-worker-contract", "v1", "valid-profile-chatbotgo.json")
}

func TestLoadProfileFileValidatesExactBytesAndIdentity(t *testing.T) {
	path := validProfilePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if err != nil {
		t.Fatalf("LoadProfileFile() error = %v", err)
	}
	digest := sha256.Sum256(raw)
	if loaded.Hash != hex.EncodeToString(digest[:]) {
		t.Fatalf("Hash = %q, want exact byte hash", loaded.Hash)
	}
	if loaded.Profile.ProfileID != "chatbotgo-contract-fixture" || len(loaded.Profile.Workers) != 5 {
		t.Fatalf("Profile = %+v", loaded.Profile)
	}
}

func TestLoadProfileFileRejectsEveryNegativeFixture(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "stack-worker-contract", "v1", "invalid-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 13 {
		t.Fatalf("negative fixtures = %d, want 13", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, loadErr := workercontract.LoadProfileFile(path, chatbotIdentity(t)); loadErr == nil {
				t.Fatal("LoadProfileFile() error = nil")
			}
		})
	}
}

func TestLoadProfileFileRejectsOversizeBOMAndSymlink(t *testing.T) {
	dir := t.TempDir()
	valid, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	oversize := filepath.Join(dir, "oversize.json")
	if err := os.WriteFile(oversize, []byte(strings.Repeat(" ", workercontract.MaxProfileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	bom := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(bom, append([]byte{0xef, 0xbb, 0xbf}, valid...), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oversize, bom, symlink, dir} {
		if _, loadErr := workercontract.LoadProfileFile(path, chatbotIdentity(t)); loadErr == nil {
			t.Fatalf("LoadProfileFile(%q) error = nil", path)
		}
	}
}

func TestDecodeSettingsRejectsUnknownDuplicateAndTrailingFields(t *testing.T) {
	type settings struct {
		Workers int `json:"workers"`
	}
	var decoded settings
	if err := workercontract.DecodeSettings([]byte(`{"workers":2}`), &decoded); err != nil || decoded.Workers != 2 {
		t.Fatalf("DecodeSettings(valid) = %+v, %v", decoded, err)
	}
	for _, raw := range []string{
		`{"workers":2,"unknown":1}`,
		`{"workers":2,"workers":3}`,
		`{"workers":2} {}`,
		`null`,
	} {
		if err := workercontract.DecodeSettings([]byte(raw), &decoded); err == nil {
			t.Fatalf("DecodeSettings(%s) error = nil", raw)
		}
	}
}

func TestProfileFileCheckerDropsReplacementIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	raw, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	checker := workercontract.NewProfileFileChecker(loaded, time.Unix(100, 0))
	if status := checker.Status(); !status.Match || status.ErrorCode != nil {
		t.Fatalf("initial status = %+v", status)
	}
	if err := os.WriteFile(path, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	status := checker.Check(time.Unix(101, 0))
	if status.Match || status.ErrorCode == nil || *status.ErrorCode != workercontract.ProfileFileChanged {
		t.Fatalf("changed status = %+v", status)
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(string(raw))), loaded.Hash) {
		t.Fatal("fixture unexpectedly contains its hash")
	}
}
