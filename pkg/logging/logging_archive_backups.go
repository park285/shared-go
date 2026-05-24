package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type archivedLogFile struct {
	path      string
	timestamp time.Time
}

func matchingCompressedBackupNames(dir, baseName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("matching compressed backup names: read dir: %w", err)
	}

	prefix, ext := backupPrefixAndExt(baseName)
	suffix := ext + compressSuffix
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		names = append(names, name)
	}

	slices.Sort(names)
	return names, nil
}

func pruneArchivedCompressedBackups(archiveDir, baseName string, maxBackups, maxAgeDays int) error {
	files, err := archivedCompressedBackups(archiveDir, baseName)
	if err != nil {
		return err
	}

	removeByPath := make(map[string]struct{})
	collectArchivedBackupsOlderThan(files, maxAgeDays, removeByPath)

	slices.SortFunc(files, func(a, b archivedLogFile) int {
		return compareArchivedLogFileNewestFirst(a, b)
	})
	collectArchivedBackupsBeyondLimit(files, maxBackups, removeByPath)

	return removeArchivedCompressedBackupPaths(removeByPath)
}

func collectArchivedBackupsOlderThan(files []archivedLogFile, maxAgeDays int, removeByPath map[string]struct{}) {
	if maxAgeDays <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	for _, file := range files {
		if file.timestamp.Before(cutoff) {
			removeByPath[file.path] = struct{}{}
		}
	}
}

func compareArchivedLogFileNewestFirst(a, b archivedLogFile) int {
	if a.timestamp.After(b.timestamp) {
		return -1
	}
	if a.timestamp.Before(b.timestamp) {
		return 1
	}
	return 0
}

func collectArchivedBackupsBeyondLimit(files []archivedLogFile, maxBackups int, removeByPath map[string]struct{}) {
	if maxBackups <= 0 || len(files) <= maxBackups {
		return
	}

	for _, file := range files[maxBackups:] {
		removeByPath[file.path] = struct{}{}
	}
}

func removeArchivedCompressedBackupPaths(removeByPath map[string]struct{}) error {
	for path := range removeByPath {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove archived backup %s: %w", filepath.Base(path), err)
		}
	}

	return nil
}

func archivedCompressedBackups(archiveDir, baseName string) ([]archivedLogFile, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("archived compressed backups: read dir: %w", err)
	}

	prefix, ext := backupPrefixAndExt(baseName)
	suffix := ext + compressSuffix
	files := make([]archivedLogFile, 0, len(entries))
	for _, entry := range entries {
		files = appendArchivedCompressedBackup(files, archiveDir, prefix, suffix, entry)
	}

	return files, nil
}

func appendArchivedCompressedBackup(
	files []archivedLogFile,
	archiveDir string,
	prefix string,
	suffix string,
	entry os.DirEntry,
) []archivedLogFile {
	if entry.IsDir() {
		return files
	}

	name := entry.Name()
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return files
	}

	timestamp, err := backupTimestampFromName(name, prefix, suffix)
	if err != nil {
		return files
	}

	return append(files, archivedLogFile{
		path:      filepath.Join(archiveDir, name),
		timestamp: timestamp,
	})
}

func backupPrefixAndExt(baseName string) (string, string) {
	ext := filepath.Ext(baseName)
	return strings.TrimSuffix(baseName, ext) + "-", ext
}

func backupTimestampFromName(name, prefix, suffix string) (time.Time, error) {
	timestamp, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return time.Time{}, fmt.Errorf("unexpected backup name: %s", name)
	}
	timestamp, ok = strings.CutSuffix(timestamp, suffix)
	if !ok {
		return time.Time{}, fmt.Errorf("unexpected backup name: %s", name)
	}

	parsed, err := time.Parse(backupTimeFormat, timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("backup timestamp from name: parse %q: %w", timestamp, err)
	}
	return parsed, nil
}
