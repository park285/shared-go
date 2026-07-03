package dbmigrate

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
)

const (
	// ManifestName은 기본 migration manifest 파일명이다.
	ManifestName = "manifest.txt"
)

// Execer는 migration SQL 실행 함수다.
type Execer func(context.Context, string) error

// SQLExecer는 database/sql 계열 ExecContext 최소 인터페이스다.
type SQLExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Option은 migration 적용 동작을 조정한다.
type Option func(*options)

type options struct {
	only map[string]bool
}

// SQLExec는 database/sql 계열 handle을 Execer로 감싼다.
func SQLExec(db SQLExecer) Execer {
	return func(ctx context.Context, query string) error {
		_, err := db.ExecContext(ctx, query)
		return err
	}
}

// WithOnly는 지정한 migration 파일명만 manifest 순서대로 적용한다.
func WithOnly(names ...string) Option {
	return func(o *options) {
		o.only = make(map[string]bool, len(names))
		for _, name := range names {
			o.only[name] = true
		}
	}
}

// Manifest는 manifest.txt를 파싱해 적용 순서대로 SQL 파일명을 반환한다.
func Manifest(fsys fs.FS) (names []string, err error) {
	file, err := fsys.Open(ManifestName)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close manifest: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("malformed manifest line %q (want \"NNN filename.sql\")", line)
		}
		names = append(names, fields[len(fields)-1])
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan manifest: %w", scanErr)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("manifest %q has no entries", ManifestName)
	}
	return names, nil
}

// Apply는 manifest 순서대로 SQL 파일을 읽어 exec로 실행한다.
func Apply(ctx context.Context, fsys fs.FS, exec Execer, opts ...Option) error {
	if exec == nil {
		return fmt.Errorf("dbmigrate: exec is required")
	}

	var cfg options
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	entries, err := Manifest(fsys)
	if err != nil {
		return fmt.Errorf("dbmigrate: %w", err)
	}

	for _, name := range entries {
		if cfg.only != nil && !cfg.only[name] {
			continue
		}

		sqlBytes, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return fmt.Errorf("dbmigrate: read %s: %w", name, readErr)
		}
		if execErr := exec(ctx, string(sqlBytes)); execErr != nil {
			return fmt.Errorf("dbmigrate: exec %s: %w", name, execErr)
		}
	}

	return nil
}
