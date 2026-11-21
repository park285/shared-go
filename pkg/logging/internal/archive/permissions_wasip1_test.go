package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExistingLogFilePermissionsUnsupported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.log")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLogFilePerm(path); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("prepare existing file: got %v, want %v", err, errors.ErrUnsupported)
	}
}
