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
	closed  bool
	lastRun time.Time

	inflight sync.WaitGroup
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
	if a.closed || a.running || (!a.lastRun.IsZero() && time.Since(a.lastRun) < ScanInterval) {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.lastRun = time.Now()
	a.inflight.Add(1)
	a.mu.Unlock()

	go a.run()
}

func (a *CompressedLogArchiver) run() {
	defer a.inflight.Done()

	err := MoveAndPrune(a.logPath, a.maxBackups, a.maxAgeDays)

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	if err != nil {
		fmt.Fprintf(os.Stderr, "log archive warning: path=%s err=%v\n", a.logPath, err)
	}
}

func (a *CompressedLogArchiver) wait() {
	if a == nil {
		return
	}
	a.inflight.Wait()
}

func (a *CompressedLogArchiver) Close() error {
	if a == nil {
		return nil
	}
	// closed를 wait 전에 세워야 진행 중 run은 join하고 이후 Trigger는 차단된다(close 후 archive dir 재생성 방지).
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.wait()
	return nil
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
		if ok, err := removeIfUnsafeCompressedBackup(source); err != nil {
			return fmt.Errorf("check compressed backup %s: %w", name, err)
		} else if !ok {
			continue
		}

		target := filepath.Join(archiveDir, name)
		if err := os.Rename(source, target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("move compressed backup %s: %w", name, err)
		}
		if ok, err := removeIfUnsafeCompressedBackup(target); err != nil {
			return fmt.Errorf("check moved compressed backup %s: %w", name, err)
		} else if !ok {
			continue
		}

		if err := os.Chmod(target, LogFilePerm); err != nil {
			fmt.Fprintf(os.Stderr, "log archive warning: chmod %s err=%v\n", target, err)
		}
	}

	if err := pruneArchivedCompressedBackups(archiveDir, filepath.Base(logPath), maxBackups, maxAgeDays); err != nil {
		return fmt.Errorf("prune archived backups: %w", err)
	}

	return nil
}

func removeIfUnsafeCompressedBackup(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode().IsRegular() {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}
