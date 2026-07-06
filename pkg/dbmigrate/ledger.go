package dbmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode"
)

const defaultLedgerTable = "schema_migrations"

// Row는 단일 SQL row scan 동작이다.
type Row interface {
	// Scan은 row 값을 destination에 복사한다.
	Scan(dest ...any) error
}

// RowQuerier는 context-aware 단일 row 조회 동작이다.
type RowQuerier interface {
	// QueryRow는 단일 row 조회를 실행한다.
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// SQLQueryRowContext는 database/sql 계열 QueryRowContext 최소 인터페이스다.
type SQLQueryRowContext interface {
	// QueryRowContext는 context-aware 단일 row 조회를 실행한다.
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Ledger는 적용된 migration 파일명을 저장하는 테이블 설정이다.
type Ledger struct {
	// Table은 ledger 테이블 이름이다.
	Table string
}

// SQLQueryRow는 database/sql 계열 handle을 RowQuerier로 감싼다.
func SQLQueryRow(db SQLQueryRowContext) RowQuerier {
	return sqlRowQuerier{db: db}
}

// WithLedger는 적용 완료 ledger를 사용해 migration을 idempotent하게 만든다.
// Apply와 Record는 별도 Execer 호출이라 원자적이지 않고, ledger는 at-least-once이므로 migration SQL은 idempotent해야 한다.
// ledger 단독은 동시 실행을 막지 못한다: 여러 마이그레이터가 같은 migration을 동시에 Applied()==false로 보고 함께 실행할 수 있다.
// 다중 레플리카에서 single-flight가 필요하면 Apply를 WithAdvisoryLock으로 감싸라.
func WithLedger(l Ledger, q RowQuerier) Option {
	return func(o *options) {
		o.ledger = &l
		o.ledgerQuerier = q
	}
}

// Ensure는 ledger 테이블을 없으면 생성한다.
func (l Ledger) Ensure(ctx context.Context, exec Execer) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}
	table, err := l.tableName()
	if err != nil {
		return err
	}
	query := queryEnsureLedger(table)
	if err := exec(ctx, query); err != nil {
		return fmt.Errorf("dbmigrate: ensure ledger: %w", err)
	}
	return nil
}

// Baseline은 지정 migration까지 ledger에 적용 완료로 기록한다.
func Baseline(ctx context.Context, fsys fs.FS, exec Execer, through string, l Ledger) error {
	if strings.TrimSpace(through) == "" {
		return errors.New("dbmigrate: baseline through migration is required")
	}

	entries, err := Manifest(fsys)
	if err != nil {
		return fmt.Errorf("dbmigrate: %w", err)
	}

	throughIndex := -1
	for i, name := range entries {
		if name == through {
			throughIndex = i
			break
		}
	}
	if throughIndex < 0 {
		return fmt.Errorf("dbmigrate: baseline through migration %q not found in manifest", through)
	}

	if err := l.Ensure(ctx, exec); err != nil {
		return err
	}
	for _, name := range entries[:throughIndex+1] {
		if err := l.Record(ctx, exec, name); err != nil {
			return err
		}
	}
	return nil
}

// Applied는 migration 파일명이 ledger에 기록되어 있는지 조회한다.
func (l Ledger) Applied(ctx context.Context, q RowQuerier, name string) (bool, error) {
	if q == nil {
		return false, errors.New("dbmigrate: ledger querier is required")
	}
	table, err := l.tableName()
	if err != nil {
		return false, err
	}

	var applied bool
	query := queryLedgerApplied(table)
	if err := q.QueryRow(ctx, query, name).Scan(&applied); err != nil {
		return false, fmt.Errorf("dbmigrate: query ledger %s: %w", name, err)
	}
	return applied, nil
}

// Record는 migration 파일명을 ledger에 idempotent하게 기록한다.
func (l Ledger) Record(ctx context.Context, exec Execer, name string) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}
	table, err := l.tableName()
	if err != nil {
		return err
	}
	query := queryRecordLedger(table)
	if err := exec(ctx, query, name); err != nil {
		return fmt.Errorf("dbmigrate: record ledger %s: %w", name, err)
	}
	return nil
}

func (l Ledger) tableName() (string, error) {
	table := strings.TrimSpace(l.Table)
	if table == "" {
		table = defaultLedgerTable
	}

	parts := strings.Split(table, ".")
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return "", fmt.Errorf("dbmigrate: invalid ledger table %q", table)
		}
		if err := validateIdentifier(trimmed); err != nil {
			return "", fmt.Errorf("dbmigrate: invalid ledger table %q: %w", table, err)
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(trimmed, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, "."), nil
}

func validateIdentifier(s string) error {
	for i, r := range s {
		switch {
		case r == '_':
		case unicode.IsLetter(r):
		case i > 0 && unicode.IsDigit(r):
		default:
			return fmt.Errorf("invalid identifier character %q", r)
		}
	}
	return nil
}

type sqlRowQuerier struct {
	db SQLQueryRowContext
}

func (q sqlRowQuerier) QueryRow(ctx context.Context, query string, args ...any) Row {
	return q.db.QueryRowContext(ctx, query, args...)
}
