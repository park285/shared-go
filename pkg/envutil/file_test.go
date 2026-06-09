package envutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringOrFile(t *testing.T) {
	t.Run("env value takes precedence", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "secret")
		require.NoError(t, os.WriteFile(filePath, []byte("from-file"), 0o600))

		t.Setenv("TEST_SOF", "from-env")
		t.Setenv("TEST_SOF_FILE", filePath)

		require.Equal(t, "from-env", StringOrFile("TEST_SOF", "def"))
	})

	t.Run("env value trimmed", func(t *testing.T) {
		require.NoError(t, os.Unsetenv("TEST_SOF_FILE"))
		t.Setenv("TEST_SOF", "  from-env  ")

		require.Equal(t, "from-env", StringOrFile("TEST_SOF", "def"))
	})

	t.Run("falls back to file when env empty", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "secret")
		require.NoError(t, os.WriteFile(filePath, []byte("  from-file\n"), 0o600))

		require.NoError(t, os.Unsetenv("TEST_SOF"))
		t.Setenv("TEST_SOF_FILE", filePath)

		require.Equal(t, "from-file", StringOrFile("TEST_SOF", "def"))
	})

	t.Run("default when neither env nor file set", func(t *testing.T) {
		require.NoError(t, os.Unsetenv("TEST_SOF"))
		require.NoError(t, os.Unsetenv("TEST_SOF_FILE"))

		require.Equal(t, "def", StringOrFile("TEST_SOF", "def"))
	})

	t.Run("default when file read fails", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "does-not-exist")

		require.NoError(t, os.Unsetenv("TEST_SOF"))
		t.Setenv("TEST_SOF_FILE", missing)

		require.Equal(t, "def", StringOrFile("TEST_SOF", "def"))
	})

	t.Run("default when file is empty", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "empty")
		require.NoError(t, os.WriteFile(filePath, []byte("   \n"), 0o600))

		require.NoError(t, os.Unsetenv("TEST_SOF"))
		t.Setenv("TEST_SOF_FILE", filePath)

		require.Equal(t, "def", StringOrFile("TEST_SOF", "def"))
	})
}
