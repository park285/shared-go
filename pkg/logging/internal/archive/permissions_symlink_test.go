package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSG05EnsureLogFilePermRejectsSymlink_efb56f99(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("sensitive"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	link := filepath.Join(dir, "log.txt")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err := EnsureLogFilePerm(link)
	if !errors.Is(err, ErrLogPathSymlink) {
		t.Fatalf("EnsureLogFilePerm(symlink) error = %v, want ErrLogPathSymlink", err)
	}

	info, statErr := os.Stat(victim)
	if statErr != nil {
		t.Fatalf("stat victim: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("victim perm = %o, want 0600 (chmod must not have followed symlink)", info.Mode().Perm())
	}
}

func TestSG05EnsureLogDirPermRejectsSymlink_efb56f99(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	victimDir := filepath.Join(dir, "victim-dir")
	if err := os.Mkdir(victimDir, 0o700); err != nil {
		t.Fatalf("mkdir victim: %v", err)
	}

	link := filepath.Join(dir, "logdir")
	if err := os.Symlink(victimDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err := EnsureLogDirPerm(link)
	if !errors.Is(err, ErrLogPathSymlink) {
		t.Fatalf("EnsureLogDirPerm(symlink) error = %v, want ErrLogPathSymlink", err)
	}

	info, statErr := os.Stat(victimDir)
	if statErr != nil {
		t.Fatalf("stat victim dir: %v", statErr)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("victim dir perm = %o, want 0700 (chmod must not have followed symlink)", info.Mode().Perm())
	}
}
