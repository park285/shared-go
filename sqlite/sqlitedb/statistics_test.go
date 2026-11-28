package sqlitedb

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func statisticsOptions(t *testing.T) Options {
	t.Helper()

	return Options{
		Path:               filepath.Join(t.TempDir(), "state", "state.db"),
		ReadConns:          1,
		ConnMaxIdleTime:    time.Minute,
		BusyTimeout:        2 * time.Second,
		JournalSizeLimit:   8 << 20,
		CheckpointInterval: time.Hour,
	}
}

func openStatistics(t *testing.T, opts Options) *DB {
	t.Helper()

	db, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := db.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	})

	return db
}

func analyzedRows(t *testing.T, db *sql.DB) int {
	t.Helper()

	var stat string

	if err := db.QueryRowContext(t.Context(), "SELECT stat FROM sqlite_stat1 WHERE idx = 't_v'").Scan(&stat); err != nil {
		t.Fatalf("statistics for t_v: %v", err)
	}

	rows, err := strconv.Atoi(strings.Fields(stat)[0])
	if err != nil {
		t.Fatalf("statistics for t_v: %q", stat)
	}

	return rows
}

func exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func TestOpenAnalyzesTablesThatLackStatistics(t *testing.T) {
	opts := statisticsOptions(t)

	first, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	exec(t, first.Writer(), "CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER NOT NULL) STRICT")
	exec(t, first.Writer(), "CREATE INDEX t_v ON t (v)")
	exec(t, first.Writer(), "INSERT INTO t (v) SELECT value % 7 FROM generate_series(1, 100)")

	if err := first.Close(t.Context()); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened := openStatistics(t, opts)

	if got := analyzedRows(t, reopened.Reader()); got != 100 {
		t.Fatalf("t_v analyzed with %d rows, want 100", got)
	}
}

func TestMigrateAnalyzesNewIndexes(t *testing.T) {
	db := openStatistics(t, statisticsOptions(t))

	err := db.Migrate(t.Context(), []Migration{
		{Name: "0001_create.sql", SQL: "CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER NOT NULL) STRICT; INSERT INTO t (v) SELECT value % 7 FROM generate_series(1, 100);"},
		{Name: "0002_index.sql", SQL: "CREATE INDEX t_v ON t (v);"},
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if got := analyzedRows(t, db.Reader()); got != 100 {
		t.Fatalf("t_v analyzed with %d rows, want 100", got)
	}
}

func TestPeriodicOptimizeReanalyzesTablesOnlyReadersQuery(t *testing.T) {
	db := openStatistics(t, statisticsOptions(t))

	exec(t, db.Writer(), "CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER NOT NULL) STRICT")
	exec(t, db.Writer(), "CREATE INDEX t_v ON t (v)")
	exec(t, db.Writer(), "INSERT INTO t (v) SELECT value % 7 FROM generate_series(1, 10)")

	if err := db.optimize(t.Context(), optimizeSQL); err != nil {
		t.Fatalf("optimize: %v", err)
	}

	if got := analyzedRows(t, db.Reader()); got != 10 {
		t.Fatalf("t_v analyzed with %d rows, want 10", got)
	}

	exec(t, db.Writer(), "INSERT INTO t (v) SELECT value % 7 FROM generate_series(1, 190)")

	if err := db.optimize(t.Context(), optimizeSQL); err != nil {
		t.Fatalf("optimize: %v", err)
	}

	if got := analyzedRows(t, db.Reader()); got < 100 {
		t.Fatalf("t_v analyzed with %d rows after growing to 200", got)
	}
}

func TestMaintenanceLoopAnalyzesTablesHourly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := openStatistics(t, statisticsOptions(t))

		exec(t, db.Writer(), "CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER NOT NULL) STRICT")
		exec(t, db.Writer(), "CREATE INDEX t_v ON t (v)")
		exec(t, db.Writer(), "INSERT INTO t (v) SELECT value % 7 FROM generate_series(1, 100)")

		time.Sleep(optimizeInterval)
		synctest.Wait()

		if got := analyzedRows(t, db.Reader()); got != 100 {
			t.Fatalf("t_v analyzed with %d rows, want 100", got)
		}
	})
}
