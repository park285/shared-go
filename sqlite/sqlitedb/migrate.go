package sqlitedb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
)

var (
	ErrMigrationDrift      = errors.New("sqlitedb: applied migration differs from the embedded migration")
	ErrUnknownMigration    = errors.New("sqlitedb: database has migrations this binary does not know")
	ErrForeignKeyViolation = errors.New("sqlitedb: migration left rows that violate foreign keys")
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

	if err := db.gate.acquire(ctx); err != nil {
		return fmt.Errorf("sqlitedb: begin: %w", withContextError(ctx, err))
	}

	defer db.gate.release()

	conn, err := db.write.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlitedb: migration connection: %w", withContextError(ctx, err))
	}

	return withForeignKeysOff(ctx, conn, func() error {
		return runTx(ctx, conn, func(tx *sql.Tx) error {
			return applyMigrations(ctx, tx, migrations)
		})
	})
}

func applyMigrations(ctx context.Context, tx *sql.Tx, migrations []Migration) error {
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

	if len(applied) == len(migrations) {
		return nil
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

	return checkForeignKeys(ctx, tx)
}

func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	var table, parent string

	err := tx.QueryRowContext(ctx, foreignKeyCheckSQL).Scan(&table, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("sqlitedb: check foreign keys: %w", err)
	}

	return fmt.Errorf("%w: %s references %s", ErrForeignKeyViolation, table, parent)
}

func withForeignKeysOff(ctx context.Context, conn *sql.Conn, fn func() error) (resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, releaseMigrationConn(context.WithoutCancel(ctx), conn))
	}()

	if _, err := conn.ExecContext(ctx, foreignKeysOffSQL); err != nil {
		return fmt.Errorf("sqlitedb: disable foreign keys for migration: %w", err)
	}

	return fn()
}

func releaseMigrationConn(ctx context.Context, conn *sql.Conn) error {
	restoreErr := restoreForeignKeys(ctx, conn)
	if restoreErr == nil {
		if err := conn.Close(); err != nil {
			return fmt.Errorf("sqlitedb: release migration connection: %w", err)
		}

		return nil
	}

	return errors.Join(fmt.Errorf("sqlitedb: restore foreign keys after migration: %w", restoreErr), discardConn(conn))
}

func discardConn(conn *sql.Conn) error {
	if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		return fmt.Errorf("sqlitedb: discard connection: %w", err)
	}

	return nil
}

func restoreForeignKeys(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, foreignKeysOnSQL); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	var enforced int64

	if err := conn.QueryRowContext(ctx, foreignKeysStateSQL).Scan(&enforced); err != nil {
		return fmt.Errorf("read foreign keys: %w", err)
	}

	if enforced != 1 {
		return fmt.Errorf("foreign_keys = %d", enforced)
	}

	return nil
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
