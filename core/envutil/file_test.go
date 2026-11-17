package envutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringOrSecretFile(t *testing.T) {
	t.Run("env value and file together are rejected", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "secret")
		require.NoError(t, os.WriteFile(filePath, []byte("from-file"), 0o600))

		t.Setenv("TEST_SOSF", "from-env")
		t.Setenv("TEST_SOSF_FILE", filePath)

		got, err := StringOrSecretFile("TEST_SOSF")
		require.EqualError(t, err, "envutil: TEST_SOSF and TEST_SOSF_FILE are both set")
		require.Empty(t, got)
	})

	t.Run("env value without file", func(t *testing.T) {
		t.Setenv("TEST_SOSF", " from-env ")
		t.Setenv("TEST_SOSF_FILE", "")

		got, err := StringOrSecretFile("TEST_SOSF")
		require.NoError(t, err)
		require.Equal(t, "from-env", got)
	})

	t.Run("empty when neither env nor file set", func(t *testing.T) {
		t.Setenv("TEST_SOSF", "")
		t.Setenv("TEST_SOSF_FILE", "")

		got, err := StringOrSecretFile("TEST_SOSF")
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
