package logging

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestEnableFileLogging_DoesNotCreateCombinedLog(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	config := Config{
		Level:      "info",
		Dir:        logDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 7,
	}

	if _, err := EnableFileLogging(config, "service.log"); err != nil {
		t.Fatalf("EnableFileLogging() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(logDir, "combined.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("combined.log stat error = %v, want %v", err, os.ErrNotExist)
	}
}

func TestArchiveCompressedLogFiles_MovesAndPrunesBackups(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), 0o644); err != nil {
		t.Fatalf("write active log failed: %v", err)
	}

	now := time.Now().UTC()
	names := []string{
		"service-" + now.Add(-48*time.Hour).Format(backupTimeFormat) + ".log.gz",
		"service-" + now.Add(-24*time.Hour).Format(backupTimeFormat) + ".log.gz",
		"service-" + now.Add(-(31*24)*time.Hour).Format(backupTimeFormat) + ".log.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write compressed backup failed: %v", err)
		}
	}

	if err := archiveCompressedLogFiles(logPath, 2, 30); err != nil {
		t.Fatalf("archiveCompressedLogFiles() error = %v", err)
	}

	archiveDir := filepath.Join(logDir, archiveDirName)
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

	if err := ensureLogFilePerm(logPath); err != nil {
		t.Fatalf("ensureLogFilePerm() error = %v", err)
	}

	assertPathPerm(t, logPath, logFilePerm)
}

func TestEnsureLogFilePerm_CreatesMissingFile(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "service.log")

	if err := ensureLogFilePerm(logPath); err != nil {
		t.Fatalf("ensureLogFilePerm() error = %v", err)
	}

	assertPathExists(t, logPath)
	assertPathPerm(t, logPath, logFilePerm)
}

func TestEnsureLogDirPerm_CreatesAndCorrectsDirectoryMode(t *testing.T) {
	t.Parallel()

	logDir := filepath.Join(t.TempDir(), "logs", "nested")
	if err := ensureLogDirPerm(logDir); err != nil {
		t.Fatalf("ensureLogDirPerm() create error = %v", err)
	}
	assertPathPerm(t, logDir, logDirPerm)

	if err := os.Chmod(logDir, 0o700); err != nil {
		t.Fatalf("chmod log dir failed: %v", err)
	}
	if err := ensureLogDirPerm(logDir); err != nil {
		t.Fatalf("ensureLogDirPerm() chmod error = %v", err)
	}
	assertPathPerm(t, logDir, logDirPerm)
}

func TestArchiveAwareWriterWrite_TriggersArchiverOnSuccessAndFailure(t *testing.T) {
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
			if err := os.WriteFile(logPath, []byte("active\n"), logFilePerm); err != nil {
				t.Fatalf("write log file failed: %v", err)
			}
			backupPath := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())

			writer := &archiveAwareWriter{
				inner:    tt.inner,
				archiver: newCompressedLogArchiver(logPath, 5, 7, true),
			}

			_, _ = writer.Write([]byte("entry\n"))

			assertPathMissing(t, backupPath)
			assertPathExists(t, filepath.Join(logDir, archiveDirName, filepath.Base(backupPath)))
		})
	}
}

func TestCompressedLogArchiverTrigger_RunsOnlyOnceWithinScanInterval(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), logFilePerm); err != nil {
		t.Fatalf("write log file failed: %v", err)
	}

	firstBackup := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC().Add(-1*time.Minute))
	archiver := newCompressedLogArchiver(logPath, 5, 7, true)
	archiver.Trigger()
	assertPathMissing(t, firstBackup)

	secondBackup := writeCompressedBackup(t, logDir, "service.log", time.Now().UTC())
	archiver.Trigger()

	assertPathExists(t, secondBackup)
	archiveEntries := archiveEntryNames(t, filepath.Join(logDir, archiveDirName))
	if len(archiveEntries) != 1 {
		t.Fatalf("archive entry count = %d, want 1; entries=%v", len(archiveEntries), archiveEntries)
	}
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
	name := fmt.Sprintf("%s%s%s%s", prefix, timestamp.Format(backupTimeFormat), ext, compressSuffix)
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
