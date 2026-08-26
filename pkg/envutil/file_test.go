package envutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringOrSecretFile(t *testing.T) {
	t.Run("env value takes precedence over file", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "secret")
		require.NoError(t, os.WriteFile(filePath, []byte("from-file"), 0o600))

		t.Setenv("TEST_SOSF", "from-env")
		t.Setenv("TEST_SOSF_FILE", filePath)

		got, err := StringOrSecretFile("TEST_SOSF", "def")
		require.NoError(t, err)
		require.Equal(t, "from-env", got)
	})

	t.Run("default when neither env nor file set", func(t *testing.T) {
		require.NoError(t, os.Unsetenv("TEST_SOSF"))
		require.NoError(t, os.Unsetenv("TEST_SOSF_FILE"))

		got, err := StringOrSecretFile("TEST_SOSF", "def")
		require.NoError(t, err)
		require.Equal(t, "def", got)
	})
}
