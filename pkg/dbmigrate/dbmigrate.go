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
type Execer func(context.Context, string, ...any) error

// SQLExecer는 database/sql 계열 ExecContext 최소 인터페이스다.
type SQLExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Option은 migration 적용 동작을 조정한다.
type Option func(*options)

type options struct {
	only          map[string]bool
	ledger        *Ledger
	ledgerQuerier RowQuerier
	session       *SessionConfig
}

// SQLExec는 database/sql 계열 handle을 Execer로 감싼다.
func SQLExec(db SQLExecer) Execer {
	return func(ctx context.Context, query string, args ...any) error {
		_, err := db.ExecContext(ctx, query, args...)
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

	cfg := applyOptions(opts)

	entries, err := Manifest(fsys)
	if err != nil {
		return fmt.Errorf("dbmigrate: %w", err)
	}
	if err := cfg.configureSession(ctx, exec); err != nil {
		return err
	}
	if err := cfg.prepareLedger(ctx, exec); err != nil {
		return err
	}

	for _, name := range entries {
		shouldApply, shouldApplyErr := cfg.shouldApply(ctx, name)
		if shouldApplyErr != nil {
			return shouldApplyErr
		}
		if !shouldApply {
			continue
		}
		if err := cfg.applyEntry(ctx, fsys, exec, name); err != nil {
			return err
		}
	}

	return nil
}

func applyOptions(opts []Option) options {
	var cfg options
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func (o options) configureSession(ctx context.Context, exec Execer) error {
	if o.session == nil {
		return nil
	}
	return o.session.Configure(ctx, exec)
}

func (o options) prepareLedger(ctx context.Context, exec Execer) error {
	if o.ledger == nil {
		return nil
	}
	if o.ledgerQuerier == nil {
		return fmt.Errorf("dbmigrate: ledger querier is required")
	}
	return o.ledger.Ensure(ctx, exec)
}

func (o options) shouldApply(ctx context.Context, name string) (bool, error) {
	if o.only != nil && !o.only[name] {
		return false, nil
	}
	if o.ledger == nil {
		return true, nil
	}

	applied, err := o.ledger.Applied(ctx, o.ledgerQuerier, name)
	if err != nil {
		return false, err
	}
	return !applied, nil
}

func (o options) applyEntry(ctx context.Context, fsys fs.FS, exec Execer, name string) error {
	sqlBytes, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("dbmigrate: read %s: %w", name, err)
	}
	if err := exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("dbmigrate: exec %s: %w", name, err)
	}
	if o.ledger != nil {
		return o.ledger.Record(ctx, exec, name)
	}
	return nil
}
