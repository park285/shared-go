package workercontract

import (
	"path/filepath"
	"strings"
	"testing"
)

func validProfilePath() string {
	return filepath.Join("testdata", "stack-worker-contract", "v1", "valid-profile-chatbotgo.json")
}

func TestLoadProfileFromEnvRejectsMissingAndPaddedPath(t *testing.T) {
	t.Setenv(ProfileFileEnv, "")

	if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil || !strings.Contains(err.Error(), "is required") {
		t.Fatalf("empty env error = %v, want required", err)
	}

	t.Setenv(ProfileFileEnv, " "+validProfilePath()+" ")

	if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil || !strings.Contains(err.Error(), "surrounding whitespace") {
		t.Fatalf("padded env error = %v, want whitespace rejection", err)
	}
}

func TestLoadProfileFromEnvLoadsFixtureForKnownIdentity(t *testing.T) {
	t.Setenv(ProfileFileEnv, validProfilePath())

	loaded, err := LoadProfileFromEnv("chatbotgo", "bot")
	if err != nil {
		t.Fatalf("LoadProfileFromEnv() error = %v", err)
	}

	if loaded.Hash == "" || loaded.Profile.Service != "chatbotgo" || loaded.Profile.Role != "bot" {
		t.Fatalf("loaded profile = service %q role %q hash %q", loaded.Profile.Service, loaded.Profile.Role, loaded.Hash)
	}

	if _, err := LoadProfileFromEnv("chatbotgo", "no-such-role"); err == nil || !strings.Contains(err.Error(), "worker identity") {
		t.Fatalf("unknown role error = %v, want identity failure", err)
	}
}
