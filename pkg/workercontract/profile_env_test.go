package workercontract

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validProfilePath() string {
	return filepath.Join("testdata", "stack-worker-contract", "v1", "valid-profile-chatbotgo.json")
}

func TestLoadProfileFromEnvRejectsMissingAndPaddedPath(t *testing.T) {
	t.Setenv(ProfileFileEnv, "")

	if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil {
		t.Fatal("empty profile path was accepted")
	}

	t.Setenv(ProfileFileEnv, " "+validProfilePath()+" ")

	if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil {
		t.Fatal("padded profile path was accepted")
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

	if _, err := LoadProfileFromEnv("chatbotgo", "no-such-role"); err == nil {
		t.Fatal("unknown role was accepted")
	}
}

func TestReleaseProfileObservesDesiredSourceWithoutLoadingIt(t *testing.T) {
	raw, err := os.ReadFile(validProfilePath())
	require.NoError(t, err)

	source := filepath.Join(t.TempDir(), "desired.json")
	require.NoError(t, os.WriteFile(source, raw, 0o600)) //nolint:gosec // 테스트가 만든 임시 디렉터리 경로만 쓴다.
	t.Setenv(ProfileFileEnv, validProfilePath())
	t.Setenv(ProfileSourceFileEnv, source)

	loaded, err := LoadProfileFromEnv("chatbotgo", "bot")
	require.NoError(t, err)
	require.Equal(t, validProfilePath(), loaded.Path())
	require.Equal(t, "chatbotgo", loaded.Profile.Service)

	now := time.Unix(100, 0)
	checker := NewProfileFileChecker(loaded, now)
	status := checker.Status()
	require.True(t, status.Match)
	require.Nil(t, status.ErrorCode)

	// 원본이 다음 버전 형식으로 바뀌어도 이전 release 설정만으로 기동한다.
	require.NoError(t, os.WriteFile(source, []byte("a different, undecodable desired profile"), 0o600))

	reloaded, err := LoadProfileFromEnv("chatbotgo", "bot")
	require.NoError(t, err)
	require.Equal(t, loaded.Hash, reloaded.Hash)

	for _, status := range []ProfileFileStatus{
		checker.Check(now.Add(time.Minute)),
		NewProfileFileChecker(reloaded, now).Status(),
	} {
		require.False(t, status.Match)
		require.Equal(t, new(ProfileFileChanged), status.ErrorCode)
	}

	require.NoError(t, os.Remove(source))

	status = NewProfileFileChecker(reloaded, now).Status()
	require.False(t, status.Match)
	require.Equal(t, new(ProfileFileMissing), status.ErrorCode)
}

func TestProfileSourceCannotReplaceInvalidRelease(t *testing.T) {
	effective := filepath.Join(t.TempDir(), "release.json")
	if err := os.WriteFile(effective, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(ProfileFileEnv, effective)
	t.Setenv(ProfileSourceFileEnv, validProfilePath())

	if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil {
		t.Fatal("valid desired source replaced an invalid release")
	}
}

func TestProfileSourceRejectsEmptyOrPaddedPath(t *testing.T) {
	t.Setenv(ProfileFileEnv, validProfilePath())

	for _, path := range []string{"", " ", " " + validProfilePath(), validProfilePath() + "\n"} {
		t.Setenv(ProfileSourceFileEnv, path)

		if _, err := LoadProfileFromEnv("chatbotgo", "bot"); err == nil {
			t.Fatal("invalid desired source path was accepted")
		}
	}
}
