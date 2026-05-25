package archive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	CompressSuffix             = ".gz"
	BackupTimeFmt              = "2006-01-02T15-04-05.000"
	DirName                    = "archive"
	ScanInterval               = 5 * time.Second
	LogFilePerm    os.FileMode = 0o640
	LogDirPerm     os.FileMode = 0o750
)

type AwareWriter struct {
	Inner    io.Writer
	Archiver *CompressedLogArchiver
}

func (w *AwareWriter) Write(p []byte) (int, error) {
	n, err := w.Inner.Write(p)
	if w.Archiver != nil {
		w.Archiver.Trigger()
	}
	if err != nil {
		return n, fmt.Errorf("archive aware writer: write: %w", err)
	}
	return n, nil
}

type CompressedLogArchiver struct {
	logPath    string
	maxBackups int
	maxAgeDays int

	mu      sync.Mutex
	running bool
	lastRun time.Time
}

func NewCompressedLogArchiver(logPath string, maxBackups, maxAgeDays int, enabled bool) *CompressedLogArchiver {
	if !enabled || strings.TrimSpace(logPath) == "" {
		return nil
	}

	return &CompressedLogArchiver{
		logPath:    logPath,
		maxBackups: maxBackups,
		maxAgeDays: maxAgeDays,
	}
}

func (a *CompressedLogArchiver) Trigger() {
	if a == nil {
		return
	}

	a.mu.Lock()
	if a.running || (!a.lastRun.IsZero() && time.Since(a.lastRun) < ScanInterval) {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.lastRun = time.Now()
	a.mu.Unlock()

	err := MoveAndPrune(a.logPath, a.maxBackups, a.maxAgeDays)

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	if err != nil {
		fmt.Fprintf(os.Stderr, "log archive warning: path=%s err=%v\n", a.logPath, err)
	}
}

func MoveAndPrune(logPath string, maxBackups, maxAgeDays int) error {
	logDir := filepath.Dir(logPath)
	archiveDir := filepath.Join(logDir, DirName)
	if err := EnsureLogDirPerm(archiveDir); err != nil {
		return fmt.Errorf("prepare archive dir: %w", err)
	}

	names, err := matchingCompressedBackupNames(logDir, filepath.Base(logPath))
	if err != nil {
		return fmt.Errorf("list compressed backups: %w", err)
	}

	for _, name := range names {
		source := filepath.Join(logDir, name)
		target := filepath.Join(archiveDir, name)
		if err := os.Rename(source, target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("move compressed backup %s: %w", name, err)
		}
	}

	if err := pruneArchivedCompressedBackups(archiveDir, filepath.Base(logPath), maxBackups, maxAgeDays); err != nil {
		return fmt.Errorf("prune archived backups: %w", err)
	}

	return nil
}
