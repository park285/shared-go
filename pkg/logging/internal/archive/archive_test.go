package archive

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestMoveAndPrune_MovesAndPrunesBackups(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), 0o644); err != nil {
		t.Fatalf("write active log failed: %v", err)
	}

	now := time.Now().UTC()
	names := []string{
		"service-" + now.Add(-48*time.Hour).Format(BackupTimeFmt) + ".log.gz",
		"service-" + now.Add(-24*time.Hour).Format(BackupTimeFmt) + ".log.gz",
		"service-" + now.Add(-(31*24)*time.Hour).Format(BackupTimeFmt) + ".log.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write compressed backup failed: %v", err)
		}
	}

	if err := MoveAndPrune(logPath, 2, 30); err != nil {
		t.Fatalf("MoveAndPrune() error = %v", err)
	}

	archiveDir := filepath.Join(logDir, DirName)
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("archive entry count = %d, want 2", len(entries))
	}
}

func TestPruneArchivedCompressedBackups_RemovesBackupsOlderThanMaxAge(t *testing.T) {
	t.Parallel()

	archiveDir := t.TempDir()
	now := time.Now().UTC()
	oldBackup := writeArchivedBackup(t, archiveDir, "service.log", now.Add(-72*time.Hour))
	recentBackup := writeArchivedBackup(t, archiveDir, "service.log", now.Add(-12*time.Hour))

	if err := pruneArchivedCompressedBackups(archiveDir, "service.log", 0, 1); err != nil {
		t.Fatalf("pruneArchivedCompressedBackups() error = %v", err)
	}

	assertPathMissing(t, oldBackup)
	assertPathExists(t, recentBackup)
}

func TestPruneArchivedCompressedBackups_RemovesBackupsBeyondMaxBackupsNewestFirst(t *testing.T) {
	t.Parallel()

	archiveDir := t.TempDir()
	now := time.Now().UTC()
	oldestBackup := writeArchivedBackup(t, archiveDir, "service.log", now.Add(-3*time.Hour))
	newerBackup := writeArchivedBackup(t, archiveDir, "service.log", now.Add(-2*time.Hour))
	newestBackup := writeArchivedBackup(t, archiveDir, "service.log", now.Add(-1*time.Hour))

	if err := pruneArchivedCompressedBackups(archiveDir, "service.log", 2, 0); err != nil {
		t.Fatalf("pruneArchivedCompressedBackups() error = %v", err)
	}

	assertPathMissing(t, oldestBackup)
	assertPathExists(t, newerBackup)
	assertPathExists(t, newestBackup)
}

func TestPruneArchivedCompressedBackups_IgnoresDisabledCriteria(t *testing.T) {
	t.Parallel()

	archiveDir := t.TempDir()
	now := time.Now().UTC()
	backups := []string{
		writeArchivedBackup(t, archiveDir, "service.log", now.Add(-72*time.Hour)),
		writeArchivedBackup(t, archiveDir, "service.log", now.Add(-48*time.Hour)),
		writeArchivedBackup(t, archiveDir, "service.log", now.Add(-24*time.Hour)),
	}

	if err := pruneArchivedCompressedBackups(archiveDir, "service.log", 0, 0); err != nil {
		t.Fatalf("pruneArchivedCompressedBackups() error = %v", err)
	}

	for _, backup := range backups {
		assertPathExists(t, backup)
	}
}

func TestEnsureLogFilePerm_CorrectsExistingFileMode(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(logPath, []byte("log\n"), 0o600); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	if err := EnsureLogFilePerm(logPath); err != nil {
		t.Fatalf("EnsureLogFilePerm() error = %v", err)
	}

	assertPathPerm(t, logPath, LogFilePerm)
}

func TestEnsureLogFilePerm_CreatesMissingFile(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "service.log")

	if err := EnsureLogFilePerm(logPath); err != nil {
		t.Fatalf("EnsureLogFilePerm() error = %v", err)
	}

	assertPathExists(t, logPath)
	assertPathPerm(t, logPath, LogFilePerm)
}

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
	cmd := exec.Command(os.Args[0], "-test.run=^TestEnsureLogFilePerm_CorrectsRestrictedUmask$")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "ARCHIVE_RESTRICTED_UMASK_HELPER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restricted-umask helper failed: %v\n%s", err, output)
	}
	assertPathPerm(t, logPath, LogFilePerm)
}

func TestEnsureLogDirPerm_CreatesAndCorrectsDirectoryMode(t *testing.T) {
	t.Parallel()

	logDir := filepath.Join(t.TempDir(), "logs", "nested")
	if err := EnsureLogDirPerm(logDir); err != nil {
		t.Fatalf("EnsureLogDirPerm() create error = %v", err)
	}
	assertPathPerm(t, logDir, LogDirPerm)

	if err := os.Chmod(logDir, 0o700); err != nil {
		t.Fatalf("chmod log dir failed: %v", err)
	}
	if err := EnsureLogDirPerm(logDir); err != nil {
		t.Fatalf("EnsureLogDirPerm() chmod error = %v", err)
	}
	assertPathPerm(t, logDir, LogDirPerm)
}

func TestAwareWriterWrite_TriggersArchiverOnSuccessAndFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		inner io.Writer
	}{
		{name: "success", inner: &bytes.Buffer{}},
		{name: "failure", inner: failingWriter{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logDir := t.TempDir()
			logPath := filepath.Join(logDir, "service.log")
			if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
				t.Fatalf("write log file failed: %v", err)
			}
			backupPath := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())

			writer := &AwareWriter{
				Inner:    tt.inner,
				Archiver: NewCompressedLogArchiver(logPath, 5, 7, true),
			}

			_, _ = writer.Write([]byte("entry\n"))
			writer.Archiver.wait()

			assertPathMissing(t, backupPath)
			assertPathExists(t, filepath.Join(logDir, DirName, filepath.Base(backupPath)))
		})
	}
}

func TestCompressedLogArchiverTrigger_RunsOnlyOnceWithinScanInterval(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	firstBackup := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC().Add(-1*time.Minute))
	archiver := NewCompressedLogArchiver(logPath, 5, 7, true)
	archiver.Trigger()
	archiver.wait()
	assertPathMissing(t, firstBackup)

	secondBackup := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())
	archiver.Trigger()
	archiver.wait()

	assertPathExists(t, secondBackup)
	archiveEntries := archiveEntryNames(t, filepath.Join(logDir, DirName))
	if len(archiveEntries) != 1 {
		t.Fatalf("archive entry count = %d, want 1; entries=%v", len(archiveEntries), archiveEntries)
	}
}

func TestCompressedLogArchiverTrigger_ReturnsBeforeScanCompletes(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}
	writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())

	release := make(chan struct{})
	entered := make(chan struct{})
	var enteredOnce sync.Once
	restore := setReadDirFn(func(name string) ([]os.DirEntry, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return os.ReadDir(name)
	})
	defer restore()

	archiver := NewCompressedLogArchiver(logPath, 5, 7, true)
	archiver.Trigger()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("MoveAndPrune never started; Trigger may be running synchronously")
	}

	close(release)
	archiver.wait()
}

func TestCompressedLogArchiverTrigger_ConcurrentRunsAtMostOnce(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}
	writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())

	release := make(chan struct{})
	var concurrent, peak atomic.Int32
	restore := setReadDirFn(func(name string) ([]os.DirEntry, error) {
		now := concurrent.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		<-release
		concurrent.Add(-1)
		return os.ReadDir(name)
	})
	defer restore()

	archiver := NewCompressedLogArchiver(logPath, 5, 7, true)

	const triggers = 16
	var wg sync.WaitGroup
	wg.Add(triggers)
	for range triggers {
		go func() {
			defer wg.Done()
			archiver.Trigger()
		}()
	}
	wg.Wait()
	close(release)
	archiver.wait()

	if got := peak.Load(); got > 1 {
		t.Fatalf("peak concurrent MoveAndPrune = %d, want at most 1", got)
	}
}

func TestCompressedLogArchiverClose_BlocksLaterTriggerFromTouchingDir(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}
	writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())

	archiver := NewCompressedLogArchiver(logPath, 5, 7, true)
	if err := archiver.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	archiver.Trigger()
	archiver.wait()

	assertPathMissing(t, filepath.Join(logDir, DirName))
}

func TestMoveAndPrune_SetsArchivedBackupPerm(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	name := fmt.Sprintf("service-%s.log.gz", time.Now().UTC().Format(BackupTimeFmt))
	backupPath := filepath.Join(logDir, name)
	if err := os.WriteFile(backupPath, []byte(name), 0o600); err != nil {
		t.Fatalf("write compressed backup failed: %v", err)
	}

	if err := MoveAndPrune(logPath, 5, 7); err != nil {
		t.Fatalf("MoveAndPrune() error = %v", err)
	}

	assertPathPerm(t, filepath.Join(logDir, DirName, name), LogFilePerm)
}

func TestMoveAndPrune_RemovesSymlinkBackupWithoutChmodTarget(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), LogFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	victimPath := filepath.Join(logDir, "victim")
	if err := os.WriteFile(victimPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write victim failed: %v", err)
	}

	name := fmt.Sprintf("service-%s.log.gz", time.Now().UTC().Format(BackupTimeFmt))
	linkPath := filepath.Join(logDir, name)
	if err := os.Symlink(victimPath, linkPath); err != nil {
		t.Fatalf("create symlink backup failed: %v", err)
	}

	if err := MoveAndPrune(logPath, 5, 7); err != nil {
		t.Fatalf("MoveAndPrune() error = %v", err)
	}

	assertPathMissing(t, linkPath)
	assertPathMissing(t, filepath.Join(logDir, DirName, name))
	assertPathPerm(t, victimPath, 0o600)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func writeArchivedBackup(t *testing.T, archiveDir, baseName string, timestamp time.Time) string {
	t.Helper()

	return writeCompressedBackup(t, archiveDir, baseName, timestamp)
}

func writeCompressedBackup(t *testing.T, dir, baseName string, timestamp time.Time) string {
	t.Helper()

	prefix, ext := backupPrefixAndExt(baseName)
	name := fmt.Sprintf("%s%s%s%s", prefix, timestamp.Format(BackupTimeFmt), ext, CompressSuffix)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatalf("write compressed backup failed: %v", err)
	}
	return path
}

func archiveEntryNames(t *testing.T, archiveDir string) []string {
	t.Helper()

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir failed: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s error = %v, want exists", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s error = %v, want %v", path, err, os.ErrNotExist)
	}
}

func assertPathPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s failed: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
	}
}
