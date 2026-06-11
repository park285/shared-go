package envutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringOrFile_ReadFailureWarns(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	require.NoError(t, os.Unsetenv("TEST_SOF_W"))
	t.Setenv("TEST_SOF_W_FILE", missing)

	var got string
	records := captureWarn(t, func() { got = StringOrFile("TEST_SOF_W", "def") })

	require.Equal(t, "def", got)
	require.Len(t, records, 1)
	require.Equal(t, "TEST_SOF_W", records[0]["key"])
	for _, rec := range records {
		for _, v := range rec {
			if s, ok := v.(string); ok {
				require.NotContains(t, s, "def")
			}
		}
	}
}

func TestStringOrFile_WorldAccessibleWarns(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o644))
	require.NoError(t, os.Chmod(filePath, 0o604))

	require.NoError(t, os.Unsetenv("TEST_SOF_W"))
	t.Setenv("TEST_SOF_W_FILE", filePath)

	var got string
	records := captureWarn(t, func() { got = StringOrFile("TEST_SOF_W", "def") })

	require.Equal(t, "topsecret", got)
	require.Len(t, records, 1)
	require.Equal(t, "TEST_SOF_W", records[0]["key"])
	for _, rec := range records {
		for _, v := range rec {
			if s, ok := v.(string); ok {
				require.NotContains(t, s, "topsecret")
			}
		}
	}
}

func TestStringOrFile_GroupReadableDoesNotWarn(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))
	require.NoError(t, os.Chmod(filePath, 0o640))

	require.NoError(t, os.Unsetenv("TEST_SOF_W"))
	t.Setenv("TEST_SOF_W_FILE", filePath)

	var got string
	records := captureWarn(t, func() { got = StringOrFile("TEST_SOF_W", "def") })

	require.Equal(t, "topsecret", got)
	require.Empty(t, records)
}

func TestStringOrFile_OwnerOnlyDoesNotWarn(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(filePath, []byte("topsecret"), 0o600))

	require.NoError(t, os.Unsetenv("TEST_SOF_W"))
	t.Setenv("TEST_SOF_W_FILE", filePath)

	records := captureWarn(t, func() { StringOrFile("TEST_SOF_W", "def") })
	require.Empty(t, records)
}
