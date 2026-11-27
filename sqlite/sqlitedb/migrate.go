package sqlitedb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
)

var (
	ErrMigrationDrift   = errors.New("sqlitedb: applied migration differs from the embedded migration")
	ErrUnknownMigration = errors.New("sqlitedb: database has migrations this binary does not know")
)

type Migration struct {
	Name string
	SQL  string
}

type appliedMigration struct {
	version int64
	name    string
	sum     []byte
}

func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: read migrations: %w", err)
	}

	var migrations []Migration

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		body, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("sqlitedb: read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{Name: entry.Name(), SQL: string(body)})
	}

	if len(migrations) == 0 {
		return nil, fmt.Errorf("sqlitedb: no migrations in %s", dir)
	}

	return migrations, nil
}

func (db *DB) Migrate(ctx context.Context, migrations []Migration) error {
	if err := validateMigrations(migrations); err != nil {
		return err
	}

	return db.WriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, createMigrationLedgerSQL); err != nil {
			return fmt.Errorf("sqlitedb: create migration ledger: %w", err)
		}

		applied, err := readLedger(ctx, tx)
		if err != nil {
			return err
		}

		if len(applied) > len(migrations) {
			return fmt.Errorf("%w: %d applied, %d known", ErrUnknownMigration, len(applied), len(migrations))
		}

		for i, row := range applied {
			sum := sha256.Sum256([]byte(migrations[i].SQL))
			if row.version != int64(i+1) || row.name != migrations[i].Name || !bytes.Equal(row.sum, sum[:]) {
				return fmt.Errorf("%w: version %d %s", ErrMigrationDrift, row.version, row.name)
			}
		}

		for i := len(applied); i < len(migrations); i++ {
			if _, err := tx.ExecContext(ctx, migrations[i].SQL); err != nil {
				return fmt.Errorf("sqlitedb: apply migration %s: %w", migrations[i].Name, err)
			}

			sum := sha256.Sum256([]byte(migrations[i].SQL))
			if _, err := tx.ExecContext(ctx, insertMigrationSQL, i+1, migrations[i].Name, sum[:], time.Now().UnixMilli()); err != nil {
				return fmt.Errorf("sqlitedb: record migration %s: %w", migrations[i].Name, err)
			}
		}

		return nil
	})
}

func validateMigrations(migrations []Migration) error {
	if len(migrations) == 0 {
		return errors.New("sqlitedb: no migrations")
	}

	seen := make(map[string]struct{}, len(migrations))
	for _, migration := range migrations {
		if migration.Name == "" || strings.TrimSpace(migration.SQL) == "" {
			return fmt.Errorf("sqlitedb: migration %q has no name or body", migration.Name)
		}

		if _, ok := seen[migration.Name]; ok {
			return fmt.Errorf("sqlitedb: duplicate migration %s", migration.Name)
		}

		seen[migration.Name] = struct{}{}
	}

	return nil
}

func readLedger(ctx context.Context, tx *sql.Tx) ([]appliedMigration, error) {
	rows, err := tx.QueryContext(ctx, selectMigrationsSQL)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: read migration ledger: %w", err)
	}
	defer rows.Close()

	var applied []appliedMigration

	for rows.Next() {
		var row appliedMigration

		if err := rows.Scan(&row.version, &row.name, &row.sum); err != nil {
			return nil, fmt.Errorf("sqlitedb: scan migration ledger: %w", err)
		}

		applied = append(applied, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitedb: read migration ledger: %w", err)
	}

	return applied, nil
}
