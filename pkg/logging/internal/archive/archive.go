package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

var ErrLogPathSymlink = errors.New("log path is a symlink")

type AwareWriter struct {
	Inner    io.Writer
	Archiver *CompressedLogArchiver
	Failures *atomic.Uint64
}

func (w *AwareWriter) Write(p []byte) (int, error) {
	n, err := w.Inner.Write(p)
	if err != nil || n != len(p) {
		w.Failures.Add(1)
	}

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

	a.mu.Lock()

	a.closed = true
	a.mu.Unlock()
	a.wait()

	return nil
}

func MoveAndPrune(logPath string, maxBackups, maxAgeDays int) error {
	if runtime.GOOS == "plan9" {
		return fmt.Errorf("compressed backup archive needs a cross-directory rename: %w", errors.ErrUnsupported)
	}

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

		observed, err := removeIfUnsafeCompressedBackup(source)
		if errors.Is(err, errCompressedBackupSkipped) {
			continue
		} else if err != nil {
			return fmt.Errorf("check compressed backup %s: %w", name, err)
		}

		if _, err = os.Lstat(strings.TrimSuffix(source, CompressSuffix)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check original of compressed backup %s: %w", name, err)
		}

		if observed.Mode() != LogFilePerm {
			if err = setLogMode(source, observed, LogFilePerm); err != nil {
				return fmt.Errorf("repair compressed backup %s: %w", name, err)
			}
		}

		target := filepath.Join(archiveDir, name)
		if err = os.Rename(source, target); err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return fmt.Errorf("move compressed backup %s: %w", name, err)
		}

		moved, err := removeIfUnsafeCompressedBackup(target)
		if errors.Is(err, errCompressedBackupSkipped) {
			continue
		} else if err != nil {
			return fmt.Errorf("check moved compressed backup %s: %w", name, err)
		}

		if !os.SameFile(observed, moved) {
			return fmt.Errorf("compressed backup %s changed while moving", name)
		}
	}

	if err := pruneArchivedCompressedBackups(archiveDir, filepath.Base(logPath), maxBackups, maxAgeDays); err != nil {
		return fmt.Errorf("prune archived backups: %w", err)
	}

	return nil
}

var errCompressedBackupSkipped = errors.New("compressed backup skipped")

func removeIfUnsafeCompressedBackup(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errCompressedBackupSkipped
		}

		return nil, fmt.Errorf("lstat: %w", err)
	}

	if info.Mode().IsRegular() {
		return info, nil
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove unsafe backup: %w", err)
	}

	return nil, errCompressedBackupSkipped
}
