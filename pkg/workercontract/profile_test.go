package workercontract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func knownIdentity(t *testing.T, service, role string) workercontract.Identity {
	t.Helper()
	identity, err := workercontract.KnownIdentity(service, role)
	if err != nil {
		t.Fatalf("KnownIdentity(%q, %q) error = %v", service, role, err)
	}
	return identity
}

func chatbotIdentity(t *testing.T) workercontract.Identity {
	t.Helper()
	return knownIdentity(t, "chatbotgo", "bot")
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
	chatbot := chatbotIdentity(t)
	collector := knownIdentity(t, "hololive", "youtube-collector")
	cases := map[string]struct {
		identity workercontract.Identity
		wantErr  string
	}{
		"invalid-duplicate-key.json":  {collector, `duplicate object member name "service"`},
		"invalid-extra-worker.json":   {collector, "workers: got [collection extra], want [collection]"},
		"invalid-missing.json":        {chatbot, "profile.profile_id: required"},
		"invalid-missing-worker.json": {chatbot, "workers: got [command], want [command compaction draw summary webhook_inbox]"},
		"invalid-mode.json":           {collector, "workers.collection.executor.attempt_timeout.mode: invalid"},
		"invalid-null-mismatch.json":  {collector, "workers.collection.executor.attempt_timeout.milliseconds: out of range"},
		"invalid-trailing-json.json":  {chatbot, "trailing JSON value"},
		"invalid-unknown-field.json":  {collector, "workers.collection.unknown: unknown field"},
		"invalid-value.json":          {collector, "enabled: json: cannot unmarshal JSON string into Go bool"},
		"invalid-wrong-role.json":     {collector, "profile identity: got hololive/unknown, want hololive/youtube-collector"},
		"invalid-wrong-service.json":  {collector, "profile identity: got unknown/youtube-collector, want hololive/youtube-collector"},
		"invalid-wrong-version.json":  {collector, "contract_version: got 2, want 1"},
		"invalid-zero.json":           {collector, "workers.collection.executor.configured_workers: out of range"},
	}
	paths, err := filepath.Glob(filepath.Join("testdata", "stack-worker-contract", "v1", "invalid-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 13 || len(cases) != len(paths) {
		t.Fatalf("negative fixtures = %d, expectations = %d, want 13 each", len(paths), len(cases))
	}
	for _, path := range paths {
		base := filepath.Base(path)
		t.Run(base, func(t *testing.T) {
			expected, ok := cases[base]
			if !ok {
				t.Fatalf("fixture %q has no declared rule expectation", base)
			}
			_, loadErr := workercontract.LoadProfileFile(path, expected.identity)
			if loadErr == nil {
				t.Fatal("LoadProfileFile() error = nil")
			}
			if !strings.Contains(loadErr.Error(), expected.wantErr) {
				t.Fatalf("LoadProfileFile() error = %v, want it to contain %q", loadErr, expected.wantErr)
			}
		})
	}
}

func TestLoadProfileFileRejectsNullEnabled(t *testing.T) {
	raw, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(raw), `"enabled": true`, `"enabled": null`, 1)
	if mutated == string(raw) {
		t.Fatal("valid fixture no longer carries an enabled literal")
	}
	path := filepath.Join(t.TempDir(), "null-enabled.json")
	if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, loadErr := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if loadErr == nil {
		t.Fatalf("LoadProfileFile() error = nil, webhook_inbox enabled = %v", loaded.Profile.Workers["webhook_inbox"].Executor.Enabled)
	}
	if want := "workers.webhook_inbox.executor.enabled: boolean required"; !strings.Contains(loadErr.Error(), want) {
		t.Fatalf("LoadProfileFile() error = %v, want it to contain %q", loadErr, want)
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
