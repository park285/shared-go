package workercontract_test

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
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

func TestFixedDurationRejectsNonfixedAndOutOfRangePolicies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy workercontract.DurationPolicy
		want   time.Duration
		fixed  bool
	}{
		{"minimum", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed, Milliseconds: new(int64(1))}, time.Millisecond, true},
		{"maximum", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed, Milliseconds: new(int64(workercontract.MaxFixedDurationMS))}, 24 * time.Hour, true},
		{"missing", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed}, 0, false},
		{"zero", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed, Milliseconds: new(int64(0))}, 0, false},
		{"negative", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed, Milliseconds: new(int64(-1))}, 0, false},
		{"above bound", workercontract.DurationPolicy{Mode: workercontract.DurationModeFixed, Milliseconds: new(int64(workercontract.MaxFixedDurationMS + 1))}, 0, false},
		{"per job", workercontract.DurationPolicy{Mode: workercontract.DurationModePerJob}, 0, false},
		{"none", workercontract.DurationPolicy{Mode: workercontract.DurationModeNone}, 0, false},
		{"nonfixed with value", workercontract.DurationPolicy{Mode: workercontract.DurationModePerJob, Milliseconds: new(int64(1))}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			duration, fixed := tc.policy.FixedDuration()
			if duration != tc.want || fixed != tc.fixed {
				t.Fatalf("FixedDuration() = (%s, %t), want (%s, %t)", duration, fixed, tc.want, tc.fixed)
			}
		})
	}
}

func TestLoadProfileFileValidatesExactBytesAndIdentity(t *testing.T) {
	path := validProfilePath()

	raw, err := os.ReadFile(path) //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 읽는다.
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

func TestLoadTwentyQProfileRequiresThreeWorkers(t *testing.T) {
	t.Parallel()

	identity := knownIdentity(t, "twentyq", "bot")
	wantIDs := []string{"pending_queue", "reply_outbox", "webhook_inbox"}

	if !slices.Equal(identity.WorkerIDs, wantIDs) {
		t.Fatalf("TwentyQ WorkerIDs = %v, want %v", identity.WorkerIDs, wantIDs)
	}

	raw, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}

	var profile workercontract.Profile

	if err := jsonv2.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}

	worker := profile.Workers["command"]

	profile.Service = identity.Service
	profile.Role = identity.Role

	for _, tc := range []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{name: "exact roster", ids: wantIDs},
		{name: "removed worker rejected", ids: append(slices.Clone(wantIDs), "player_registration"), wantErr: true},
		{name: "missing worker rejected", ids: wantIDs[:2], wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := profile

			profile.Workers = make(map[string]workercontract.WorkerProfile, len(tc.ids))

			for _, id := range tc.ids {
				profile.Workers[id] = worker
			}

			data, err := jsonv2.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(t.TempDir(), "twentyq.json")
			if err = os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			loaded, err := workercontract.LoadProfileFile(path, identity)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "workers: got") {
					t.Fatalf("LoadProfileFile() error = %v, want exact worker-set rejection", err)
				}
			} else if err != nil || len(loaded.Profile.Workers) != 3 {
				t.Fatalf("LoadProfileFile() = %+v, %v; want three-worker profile", loaded.Profile, err)
			}
		})
	}
}

func TestLoadProfileFileRejectsEveryNegativeFixture(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "stack-worker-contract", "v1", "invalid-*.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := workercontract.LoadProfileFile(path, chatbotIdentity(t)); err == nil {
				t.Fatal("invalid common profile accepted")
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
	if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
		t.Fatal(err)
	}

	loaded, loadErr := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if loadErr == nil {
		t.Fatalf("LoadProfileFile() error = nil, webhook_inbox enabled = %v", loaded.Profile.Workers["webhook_inbox"].Executor.Enabled)
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
	if err := os.WriteFile(bom, append([]byte{0xef, 0xbb, 0xbf}, valid...), 0o600); err != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
		t.Fatal(err)
	}

	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, valid, 0o600); err != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
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

	if writeErr := os.WriteFile(path, raw, 0o600); writeErr != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
		t.Fatal(writeErr)
	}

	loaded, err := workercontract.LoadProfileFile(path, chatbotIdentity(t))
	if err != nil {
		t.Fatal(err)
	}

	checker := workercontract.NewProfileFileChecker(loaded, time.Unix(100, 0))
	if status := checker.Status(); !status.Match || status.ErrorCode != nil {
		t.Fatalf("initial status = %+v", status)
	}

	if err := os.WriteFile(path, append(raw, ' '), 0o600); err != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
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

func TestProfileCheckerReportsRegularContentChanges(t *testing.T) {
	raw, err := os.ReadFile(validProfilePath())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"BOM", append([]byte{0xef, 0xbb, 0xbf}, raw...)},
		{"oversize", []byte(strings.Repeat("x", workercontract.MaxProfileBytes+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil { //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
				t.Fatal(err)
			}

			loaded, err := workercontract.LoadProfileFile(path, chatbotIdentity(t))
			if err != nil {
				t.Fatal(err)
			}

			checker := workercontract.NewProfileFileChecker(loaded, time.Unix(100, 0))

			if err := os.WriteFile(path, tc.raw, 0o600); err != nil {
				t.Fatal(err)
			}

			status := checker.Check(time.Unix(101, 0))
			if status.Match || status.ErrorCode == nil || *status.ErrorCode != workercontract.ProfileFileChanged {
				t.Fatalf("regular content replacement classified as file type: %+v", status)
			}

			if _, err := workercontract.LoadProfileFile(path, chatbotIdentity(t)); err == nil {
				t.Fatal("invalid replacement accepted at startup")
			}
		})
	}
}
