//go:build unix

package envutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretFile_GroupReadableFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("  from-file\n"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o640))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)
	t.Setenv("TEST_SECRET_FILE", "from-env")

	got, err := SecretFile("TEST_SECRET_FILE")
	require.NoError(t, err)
	require.Equal(t, "from-file", got)
}

func TestSecretFile_OwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("from-file"), 0o600))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.NoError(t, err)
	require.Equal(t, "from-file", got)
}

func TestSecretFile_MissingFileEnv(t *testing.T) {
	require.NoError(t, os.Unsetenv("TEST_SECRET_FILE_FILE"))

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
}

func TestSecretFile_EmptyFileEnv(t *testing.T) {
	t.Setenv("TEST_SECRET_FILE_FILE", " \n\t ")

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
}

func TestSecretFile_MissingFilePath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	t.Setenv("TEST_SECRET_FILE_FILE", missing)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
	require.Contains(t, err.Error(), missing)
}

func TestSecretFile_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target")
	linkPath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(targetPath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Symlink(targetPath, linkPath))

	t.Setenv("TEST_SECRET_FILE_FILE", linkPath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
	require.Contains(t, err.Error(), linkPath)
	require.NotContains(t, err.Error(), "topsecret")
}

func TestSecretFile_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_SECRET_FILE_FILE", dir)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "regular file")
}

func TestSecretFile_RejectsWorldAccessibleFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o644))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
	require.Contains(t, err.Error(), filePath)
	require.Contains(t, err.Error(), "-rw-r--r--")
	require.NotContains(t, err.Error(), "topsecret")
}

func TestSecretFile_RejectsInsecureModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o660, 0o620, 0o700, 0o602, 0o606} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "secret")
			require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
			require.NoError(t, os.Chmod(filePath, mode))

			t.Setenv("TEST_SECRET_FILE_FILE", filePath)

			got, err := SecretFile("TEST_SECRET_FILE")
			require.Error(t, err)
			require.Empty(t, got)
			require.NotContains(t, err.Error(), "topsecret")
		})
	}
}

func TestSecretFile_RejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte(" \n\t "), 0o600))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "TEST_SECRET_FILE_FILE")
	require.Contains(t, err.Error(), filePath)
}

func TestSecretFile_ErrorsDoNotLeakSecretContents(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("do-not-leak-this-secret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o644))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.NotContains(t, err.Error(), "do-not-leak-this-secret")
}

func TestStringOrSecretFile_ReadsSecureFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("  from-file\n"), 0o600))

	require.NoError(t, os.Unsetenv("TEST_SOSF_UNIX"))
	t.Setenv("TEST_SOSF_UNIX_FILE", filePath)

	got, err := StringOrSecretFile("TEST_SOSF_UNIX", "def")
	require.NoError(t, err)
	require.Equal(t, "from-file", got)
}

func TestStringOrSecretFile_FailClosedOnInsecureFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o644))

	require.NoError(t, os.Unsetenv("TEST_SOSF_UNIX"))
	t.Setenv("TEST_SOSF_UNIX_FILE", filePath)

	got, err := StringOrSecretFile("TEST_SOSF_UNIX", "def")
	require.Error(t, err)
	require.Empty(t, got)
	require.NotContains(t, err.Error(), "topsecret")
}

func TestFirstStringOrSecretFile_SkipsUnsetKeyToSecureFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("from-b-file"), 0o600))

	for _, k := range []string{"FIRST_A", "FIRST_A_FILE", "FIRST_B", "FIRST_B_FILE"} {
		require.NoError(t, os.Unsetenv(k))
	}
	t.Setenv("FIRST_B_FILE", filePath)

	got, err := FirstStringOrSecretFile([]string{"FIRST_A", "FIRST_B"}, "def")
	require.NoError(t, err)
	require.Equal(t, "from-b-file", got)
}

func TestFirstStringOrSecretFile_FailClosedOnConfiguredInsecureFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o644))

	for _, k := range []string{"FIRST_A", "FIRST_A_FILE", "FIRST_B", "FIRST_B_FILE"} {
		require.NoError(t, os.Unsetenv(k))
	}
	t.Setenv("FIRST_A_FILE", filePath)
	t.Setenv("FIRST_B", "fallback-b")

	got, err := FirstStringOrSecretFile([]string{"FIRST_A", "FIRST_B"}, "def")
	require.Error(t, err)
	require.Empty(t, got)
	require.NotContains(t, err.Error(), "topsecret")
}
