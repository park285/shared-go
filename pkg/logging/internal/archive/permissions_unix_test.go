//go:build unix

package archive

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEnsureLogFilePerm_CorrectsRestrictedUmask(t *testing.T) {
	if os.Getenv("ARCHIVE_RESTRICTED_UMASK_HELPER") == "1" {
		syscall.Umask(0o077)

		logPath := "service.log"
		if err := EnsureLogFilePerm(logPath); err != nil {
			t.Fatalf("EnsureLogFilePerm() error = %v", err)
		}

		assertPathPerm(t, logPath, LogFilePerm)

		return
	}

	workDir := t.TempDir()
	logPath := filepath.Join(workDir, "service.log")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestEnsureLogFilePerm_CorrectsRestrictedUmask$") //nolint:gosec // 현재 테스트 실행 파일을 고정된 테스트 이름으로 다시 실행한다.

	cmd.Dir = workDir

	cmd.Env = append(os.Environ(), "ARCHIVE_RESTRICTED_UMASK_HELPER=1")

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restricted-umask helper failed: %v\n%s", err, output)
	}

	assertPathPerm(t, logPath, LogFilePerm)
}

func TestEnsureLogDirPerm_PreservesSetgid(t *testing.T) {
	for _, initial := range []os.FileMode{os.ModeSetgid | LogDirPerm, os.ModeSetgid | 0o700} {
		dir := filepath.Join(t.TempDir(), "logs")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(dir, initial); err != nil {
			t.Fatal(err)
		}

		if err := EnsureLogDirPerm(dir); err != nil {
			t.Fatalf("EnsureLogDirPerm(%v) error = %v", initial, err)
		}

		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}

		if want := os.ModeDir | os.ModeSetgid | LogDirPerm; info.Mode() != want {
			t.Fatalf("EnsureLogDirPerm(%v) mode = %v, want %v", initial, info.Mode(), want)
		}

		logPath := filepath.Join(dir, "service.log")
		if err := EnsureLogFilePerm(logPath); err != nil {
			t.Fatalf("EnsureLogFilePerm() error = %v", err)
		}

		dirStat, fileStat := statOwner(t, dir), statOwner(t, logPath)
		if dirStat.Gid != fileStat.Gid {
			t.Fatalf("log file gid = %d, want directory gid %d", fileStat.Gid, dirStat.Gid)
		}
	}
}

func statOwner(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s: no syscall.Stat_t", path)
	}

	return stat
}

func TestRequireLogMode_AcceptsSetgidDroppedByKernel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, LogDirPerm); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, LogDirPerm); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := requireLogMode(dir, info, os.ModeDir|os.ModeSetgid|LogDirPerm); err != nil {
		t.Fatalf("requireLogMode() error = %v", err)
	}

	if err := requireLogMode(dir, info, os.ModeDir|os.ModeSticky|LogDirPerm); err == nil {
		t.Fatal("requireLogMode() accepted a missing sticky bit")
	}
}

func TestEnsureLogFilePerm_ClearsSpecialBits(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(logPath, nil, LogFilePerm); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(logPath, LogFilePerm|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(logPath); err != nil || info.Mode()&os.ModeSetgid == 0 {
		t.Skipf("setgid could not be set on a regular file: %v", err)
	}

	if err := EnsureLogFilePerm(logPath); err != nil {
		t.Fatalf("EnsureLogFilePerm() error = %v", err)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode() != LogFilePerm {
		t.Fatalf("mode = %v, want %v", info.Mode(), LogFilePerm)
	}
}

func TestEnsureLogFilePerm_RepairsOwnerUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file read permission")
	}

	logPath := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(logPath, nil, 0o200); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(logPath, 0o200); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLogFilePerm(logPath); err != nil {
		t.Fatalf("EnsureLogFilePerm() error = %v", err)
	}

	assertPathPerm(t, logPath, LogFilePerm)
}

func TestSetLogMode_RejectsReplacedPath(t *testing.T) {
	dir := t.TempDir()
	observedPath, replacedPath := filepath.Join(dir, "observed.log"), filepath.Join(dir, "replaced.log")

	for _, path := range []string{observedPath, replacedPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	observed, err := os.Lstat(observedPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := setLogMode(replacedPath, observed, LogFilePerm); err == nil {
		t.Fatal("setLogMode() accepted a path that no longer names the observed file")
	}

	assertPathPerm(t, replacedPath, 0o600)
}

func TestEnsureLogFilePerm_RepairsInaccessibleFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(logPath, 0); err != nil {
		t.Fatal(err)
	}

	assertRepair(t, logPath, 0, LogFilePerm, EnsureLogFilePerm(logPath))
}

func TestEnsureLogDirPerm_RepairsUnreadableDir(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(logDir, 0o300); err != nil {
		t.Fatal(err)
	}

	assertPathPerm(t, logDir, 0o300)
	assertRepair(t, logDir, 0o300, LogDirPerm, EnsureLogDirPerm(logDir))
}

func assertRepair(t *testing.T, path string, before, want os.FileMode, err error) {
	t.Helper()

	if !inaccessibleRepairSupported() {
		if err == nil {
			t.Fatal("permission repair succeeded where no-follow chmod of an unopenable path is unsupported")
		}

		assertPathPerm(t, path, before)

		return
	}

	if err != nil {
		t.Fatalf("permission repair error = %v", err)
	}

	assertPathPerm(t, path, want)
}
