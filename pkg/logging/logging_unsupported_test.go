//go:build windows || wasip1 || js

package logging

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileLoggingUnsupported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	config := Config{Dir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}

	logger, closer, err := EnableFileLoggingWithOptions(config, "service.log", Options{})
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}

	if !errors.Is(err, errors.ErrUnsupported) || logger != nil || closer != nil {
		t.Errorf("EnableFileLoggingWithOptions() = (%v, %v, %v), want (nil, nil, %v)", logger, closer, err, errors.ErrUnsupported)
	}

	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat log directory after rejection: got %v, want %v", err, os.ErrNotExist)
	}
}

func TestFileLoggingUnsupportedInExistingDir(t *testing.T) {
	dir := t.TempDir()
	config := Config{Dir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}

	_, closer, err := EnableFileLoggingWithOptions(config, "service.log", Options{AsyncStdout: true})
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}

	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("file logging in existing directory: got %v, want %v", err, errors.ErrUnsupported)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("entries after unsupported file logging: %v", entries)
	}
}
