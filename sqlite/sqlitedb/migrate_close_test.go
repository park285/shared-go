package sqlitedb_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"
)

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int

	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	return count
}

func TestMigrateRebuildingAParentKeepsItsChildren(t *testing.T) {
	db := openTest(t, testOptions(t))
	ctx := t.Context()

	create := sqlitedb.Migration{Name: "0001_create.sql", SQL: `
CREATE TABLE parent (id INTEGER PRIMARY KEY, label TEXT NOT NULL) STRICT;
CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE) STRICT;
INSERT INTO parent VALUES (1, 'a');
INSERT INTO child VALUES (10, 1);
`}

	if err := db.Migrate(ctx, []sqlitedb.Migration{create}); err != nil {
		t.Fatalf("create: %v", err)
	}

	rebuild := sqlitedb.Migration{Name: "0002_rebuild.sql", SQL: `
CREATE TABLE parent_next (id INTEGER PRIMARY KEY, label TEXT NOT NULL CHECK (label <> '')) STRICT;
INSERT INTO parent_next (id, label) SELECT id, label FROM parent;
DROP TABLE parent;
ALTER TABLE parent_next RENAME TO parent;
`}

	if err := db.Migrate(ctx, []sqlitedb.Migration{create, rebuild}); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if children := countRows(t, db.Reader(), "child"); children != 1 {
		t.Fatalf("children after rebuilding the parent = %d, want 1", children)
	}

	if enforced := pragma[int](t, db.Writer(), "foreign_keys"); enforced != 1 {
		t.Fatalf("writer foreign_keys after migrate = %d, want 1", enforced)
	}
}

func TestMigrateRejectsForeignKeyViolations(t *testing.T) {
	db := openTest(t, testOptions(t))
	ctx := t.Context()

	create := sqlitedb.Migration{Name: "0001_create.sql", SQL: `
CREATE TABLE parent (id INTEGER PRIMARY KEY) STRICT;
CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id)) STRICT;
`}
	orphan := sqlitedb.Migration{Name: "0002_orphan.sql", SQL: "INSERT INTO child VALUES (10, 99);"}

	if err := db.Migrate(ctx, []sqlitedb.Migration{create}); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := db.Migrate(ctx, []sqlitedb.Migration{create, orphan})
	if !errors.Is(err, sqlitedb.ErrForeignKeyViolation) {
		t.Fatalf("migrate with an orphan row = %v, want ErrForeignKeyViolation", err)
	}

	if applied := countRows(t, db.Reader(), "sqlitedb_migrations"); applied != 1 {
		t.Fatalf("ledger rows after a rejected migration = %d, want 1", applied)
	}

	if orphans := countRows(t, db.Reader(), "child"); orphans != 0 {
		t.Fatalf("child rows after a rejected migration = %d, want 0", orphans)
	}

	if enforced := pragma[int](t, db.Writer(), "foreign_keys"); enforced != 1 {
		t.Fatalf("writer foreign_keys after a rejected migration = %d, want 1", enforced)
	}

	if _, execErr := db.Writer().ExecContext(ctx, "CREATE TABLE p (id INTEGER PRIMARY KEY) STRICT; CREATE TABLE c (p INTEGER REFERENCES p(id)) STRICT"); execErr != nil {
		t.Fatal(execErr)
	}

	if _, execErr := db.Writer().ExecContext(ctx, "INSERT INTO c VALUES (5)"); execErr == nil {
		t.Fatal("writer accepted an orphan row after a rejected migration")
	}
}

func TestCloseDoesNotWaitForAttachedReaders(t *testing.T) {
	opts := testOptions(t)

	owner, err := sqlitedb.Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	ctx := t.Context()

	if _, execErr := owner.Writer().ExecContext(ctx, "CREATE TABLE t (v INTEGER) STRICT; INSERT INTO t VALUES (1)"); execErr != nil {
		t.Fatal(execErr)
	}

	other, err := sqlitedb.Attach(ctx, opts)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := other.Close(context.WithoutCancel(ctx)); closeErr != nil {
			t.Errorf("close attached: %v", closeErr)
		}
	})

	snapshot, err := other.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin attached reader: %v", err)
	}

	if rows := countRowsTx(t, snapshot); rows != 1 {
		t.Fatalf("attached snapshot rows = %d", rows)
	}

	if _, execErr := owner.Writer().ExecContext(ctx, "INSERT INTO t VALUES (2)"); execErr != nil {
		t.Fatal(execErr)
	}

	started := time.Now()
	closeErr := owner.Close(ctx)
	waited := time.Since(started)

	if rollbackErr := snapshot.Rollback(); rollbackErr != nil {
		t.Fatalf("end attached reader: %v", rollbackErr)
	}

	if closeErr != nil {
		t.Fatalf("close while an attached reader holds a snapshot: %v", closeErr)
	}

	if waited >= opts.BusyTimeout/2 {
		t.Fatalf("close waited %s for the attached reader", waited)
	}

	if rows := countRows(t, other.Reader(), "t"); rows != 2 {
		t.Fatalf("rows after close = %d, want 2", rows)
	}
}

func TestCloseReportsReadersThisProcessLeftOpen(t *testing.T) {
	opts := testOptions(t)

	owner, err := sqlitedb.Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	ctx := t.Context()

	if _, execErr := owner.Writer().ExecContext(ctx, "CREATE TABLE t (v INTEGER) STRICT; INSERT INTO t VALUES (1)"); execErr != nil {
		t.Fatal(execErr)
	}

	snapshot, err := owner.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader: %v", err)
	}

	if rows := countRowsTx(t, snapshot); rows != 1 {
		t.Fatalf("snapshot rows = %d", rows)
	}

	if _, execErr := owner.Writer().ExecContext(ctx, "INSERT INTO t VALUES (2)"); execErr != nil {
		t.Fatal(execErr)
	}

	started := time.Now()
	closeErr := owner.Close(ctx)
	waited := time.Since(started)

	if rollbackErr := snapshot.Rollback(); rollbackErr != nil {
		t.Fatalf("end reader: %v", rollbackErr)
	}

	if !errors.Is(closeErr, sqlitedb.ErrCheckpointIncomplete) {
		t.Fatalf("close with a reader of this process open = %v, want ErrCheckpointIncomplete", closeErr)
	}

	if waited >= opts.BusyTimeout/2 {
		t.Fatalf("close waited %s for its own reader", waited)
	}
}

func countRowsTx(t *testing.T, tx *sql.Tx) int {
	t.Helper()

	var count int

	if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatalf("count in snapshot: %v", err)
	}

	return count
}
