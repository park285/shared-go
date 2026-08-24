//go:build unix

package envutil

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

func TestLoadDotenvFileStrictRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")

	if err := os.WriteFile(target, []byte("STRICT_SYMLINK_KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink dotenv: %v", err)
	}

	testsupport.UnsetEnvOnCleanup(t, "STRICT_SYMLINK_KEY")

	err := LoadDotenvFile(link, true, true)
	if err == nil || !strings.Contains(err.Error(), "must not be symlink") {
		t.Fatalf("LoadDotenvFile(strict symlink) error = %v, want symlink rejection", err)
	}

	if got := os.Getenv("STRICT_SYMLINK_KEY"); got != "" {
		t.Fatalf("STRICT_SYMLINK_KEY = %q, want empty", got)
	}
}

func TestLoadDotenvFileNonStrictFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.env")

	if err := os.WriteFile(target, []byte("LOCAL_SYMLINK_KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink dotenv: %v", err)
	}

	testsupport.UnsetEnvOnCleanup(t, "LOCAL_SYMLINK_KEY")

	if err := LoadDotenvFile(link, true, false); err != nil {
		t.Fatalf("LoadDotenvFile(non-strict symlink) error = %v", err)
	}

	if got := os.Getenv("LOCAL_SYMLINK_KEY"); got != "value" {
		t.Fatalf("LOCAL_SYMLINK_KEY = %q, want value", got)
	}
}

func TestLoadDotenvFileStrictRejectsFifoWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.env")

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)

	go func() { done <- LoadDotenvFile(fifo, true, true) }()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "must be regular file") {
			t.Fatalf("LoadDotenvFile(strict fifo) error = %v, want regular file rejection", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadDotenvFile(strict fifo) blocked; startup must not wait on a FIFO writer")
	}
}

func TestOpenSecretFileNoFollowDoesNotBlockOnFifo(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		file, err := openSecretFileNoFollow(fifo)
		if file != nil {
			_ = file.Close()
		}

		done <- err
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("openSecretFileNoFollow blocked on FIFO; O_NONBLOCK guard is missing")
	}
}

func TestLoadDotenvFileStrictRejectsSwappedFile(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.env")
	replacement := filepath.Join(dir, "replacement.env")

	if err := os.WriteFile(original, []byte("SWAP_KEY=original\n"), 0o600); err != nil {
		t.Fatalf("write original dotenv: %v", err)
	}

	if err := os.WriteFile(replacement, []byte("SWAP_KEY=replacement\n"), 0o600); err != nil {
		t.Fatalf("write replacement dotenv: %v", err)
	}

	testsupport.UnsetEnvOnCleanup(t, "SWAP_KEY")

	staleInfo, err := os.Lstat(original)
	if err != nil {
		t.Fatalf("lstat original dotenv: %v", err)
	}

	if renameErr := os.Rename(replacement, original); renameErr != nil {
		t.Fatalf("rename dotenv: %v", renameErr)
	}

	err = loadStrictDotenvFile(original, staleInfo)
	if err == nil || !strings.Contains(err.Error(), "changed while opening") {
		t.Fatalf("loadStrictDotenvFile(swapped) error = %v, want swap rejection", err)
	}

	if got := os.Getenv("SWAP_KEY"); got != "" {
		t.Fatalf("SWAP_KEY = %q, want empty", got)
	}
}

func TestLoadDotenvFileStrictRejectsModeChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mode.env")

	if err := os.WriteFile(path, []byte("MODE_KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat dotenv: %v", err)
	}

	if chmodErr := os.Chmod(path, 0o646); chmodErr != nil { //nolint:gosec // 허용적인 권한을 감지하는 동작을 검증하려고 일부러 그 권한을 만든다.
		t.Fatalf("chmod dotenv: %v", chmodErr)
	}

	testsupport.UnsetEnvOnCleanup(t, "MODE_KEY")

	err = loadStrictDotenvFile(path, info)
	if err == nil || !strings.Contains(err.Error(), "world-accessible") {
		t.Fatalf("loadStrictDotenvFile(mode changed) error = %v, want world-accessible rejection", err)
	}

	if got := os.Getenv("MODE_KEY"); got != "" {
		t.Fatalf("MODE_KEY = %q, want empty", got)
	}
}

func TestLoadDotenvFileStrictLoadsRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.env")

	if err := os.WriteFile(path, []byte("STRICT_OK_KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	testsupport.UnsetEnvOnCleanup(t, "STRICT_OK_KEY")

	if err := LoadDotenvFile(path, true, true); err != nil {
		t.Fatalf("LoadDotenvFile(strict) error = %v", err)
	}

	if got := os.Getenv("STRICT_OK_KEY"); got != "value" {
		t.Fatalf("STRICT_OK_KEY = %q, want value", got)
	}
}
