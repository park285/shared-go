//go:build !unix

package envutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretFileUnsupportedWithoutNoFollowOpen(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o600))

	t.Setenv("TEST_SECRET_FILE_FILE", filePath)

	got, err := SecretFile("TEST_SECRET_FILE")
	require.Error(t, err)
	require.Empty(t, got)
	require.NotContains(t, err.Error(), "topsecret")
}

func TestOpenSecretFileNoFollowUnsupported(t *testing.T) {
	file, err := openSecretFileNoFollow("ignored")
	require.ErrorIs(t, err, errSecretFileNoFollowUnsupported)
	require.True(t, errors.Is(err, errSecretFileNoFollowUnsupported))
	require.Nil(t, file)
}
