package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const defaultLedgerTable = "schema_migrations"

type Row interface {
	Scan(dest ...any) error
}

type RowQuerier interface {
	QueryRow(ctx context.Context, query string, args ...any) Row
}

type Ledger struct {
	Table string
}

func (l Ledger) Ensure(ctx context.Context, exec Execer) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	table, err := l.tableName()
	if err != nil {
		return fmt.Errorf("dbmigrate: table name: %w", err)
	}

	query := queryEnsureLedger(table)
	if err := exec(ctx, query); err != nil {
		return fmt.Errorf("dbmigrate: ensure ledger: %w", err)
	}

	return nil
}

func (l Ledger) Applied(ctx context.Context, q RowQuerier, name string) (bool, error) {
	if q == nil {
		return false, errors.New("dbmigrate: ledger querier is required")
	}

	table, err := l.tableName()
	if err != nil {
		return false, fmt.Errorf("dbmigrate: table name: %w", err)
	}

	var applied bool

	query := queryLedgerApplied(table)

	if err := q.QueryRow(ctx, query, name).Scan(&applied); err != nil {
		return false, fmt.Errorf("dbmigrate: query ledger %s: %w", name, err)
	}

	return applied, nil
}

func (l Ledger) Record(ctx context.Context, exec Execer, name string) error {
	if exec == nil {
		return errors.New("dbmigrate: exec is required")
	}

	table, err := l.tableName()
	if err != nil {
		return fmt.Errorf("dbmigrate: table name: %w", err)
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
