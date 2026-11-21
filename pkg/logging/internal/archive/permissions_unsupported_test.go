//go:build windows || wasip1 || js

package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLogPermissionsUnsupported(t *testing.T) {
	parent := t.TempDir()

	for name, ensure := range map[string]func(string) error{
		"service.log": EnsureLogFilePerm,
		"logs":        EnsureLogDirPerm,
	} {
		path := filepath.Join(parent, name)
		if err := ensure(path); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("prepare %s: got %v, want %v", name, err, errors.ErrUnsupported)
		}

		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stat %s after rejection: got %v, want %v", name, err, os.ErrNotExist)
		}
	}
}
