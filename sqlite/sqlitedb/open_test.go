package sqlitedb_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"
)

func testOptions(t *testing.T) sqlitedb.Options {
	t.Helper()

	return sqlitedb.Options{
		Path:               filepath.Join(t.TempDir(), "state", "state.db"),
		ReadConns:          2,
		ConnMaxIdleTime:    time.Minute,
		BusyTimeout:        2 * time.Second,
		JournalSizeLimit:   8 << 20,
		CheckpointInterval: time.Hour,
	}
}

func openTest(t *testing.T, opts sqlitedb.Options) *sqlitedb.DB {
	t.Helper()

	db, err := sqlitedb.Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	return db
}

func pragma[T any](t *testing.T, db *sql.DB, name string) T {
	t.Helper()

	var value T

	if err := db.QueryRowContext(t.Context(), "PRAGMA "+name).Scan(&value); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}

	return value
}

func TestOpenAppliesDurableSettings(t *testing.T) {
	opts := testOptions(t)
	db := openTest(t, opts)

	if got := pragma[string](t, db.Writer(), "journal_mode"); got != "wal" {
		t.Fatalf("journal_mode = %q", got)
	}

	for name, want := range map[string]int64{
		"synchronous":        2,
		"foreign_keys":       1,
		"journal_size_limit": opts.JournalSizeLimit,
		"busy_timeout":       opts.BusyTimeout.Milliseconds(),
		"query_only":         0,
	} {
		if got := pragma[int64](t, db.Writer(), name); got != want {
			t.Fatalf("writer %s = %d, want %d", name, got, want)
		}
	}

	if got := pragma[int64](t, db.Reader(), "query_only"); got != 1 {
		t.Fatalf("reader query_only = %d", got)
	}

	if got := pragma[int64](t, db.Reader(), "foreign_keys"); got != 1 {
		t.Fatalf("reader foreign_keys = %d", got)
	}

	if got := db.Writer().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("writer max open connections = %d", got)
	}

	if got := db.Reader().Stats().MaxOpenConnections; got != opts.ReadConns {
		t.Fatalf("reader max open connections = %d", got)
	}
}

func TestReaderRejectsWrites(t *testing.T) {
	db := openTest(t, testOptions(t))

	if _, err := db.Writer().ExecContext(t.Context(), "CREATE TABLE t (v INTEGER) STRICT"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Reader().ExecContext(t.Context(), "INSERT INTO t VALUES (1)"); err == nil {
		t.Fatal("reader insert succeeded")
	}
}

func TestOpenAdmitsOneOwner(t *testing.T) {
	opts := testOptions(t)

	first, err := sqlitedb.Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}

	if _, secondErr := sqlitedb.Open(t.Context(), opts); !errors.Is(secondErr, sqlitedb.ErrInUse) {
		t.Fatalf("second open error = %v, want ErrInUse", secondErr)
	}

	if closeErr := first.Close(t.Context()); closeErr != nil {
		t.Fatal(closeErr)
	}

	second, err := sqlitedb.Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("reopen after close: %v", err)
	}

	if err := second.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAttachSharesTheOwnersDatabase(t *testing.T) {
	opts := testOptions(t)
	owner := openTest(t, opts)
	ctx := t.Context()

	if _, err := owner.Writer().ExecContext(ctx, "CREATE TABLE t (v INTEGER) STRICT"); err != nil {
		t.Fatal(err)
	}

	guest, err := sqlitedb.Attach(ctx, opts)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := guest.WriteTx(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx, "INSERT INTO t VALUES (7)"); execErr != nil {
			return fmt.Errorf("insert through guest: %w", execErr)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := guest.Close(ctx); err != nil {
		t.Fatal(err)
	}

	var got int

	if err := owner.Reader().QueryRowContext(ctx, "SELECT v FROM t").Scan(&got); err != nil {
		t.Fatal(err)
	}

	if got != 7 {
		t.Fatalf("owner read %d", got)
	}
}

func TestAttachRequiresAnExistingDatabase(t *testing.T) {
	opts := testOptions(t)
	if err := os.Mkdir(filepath.Dir(opts.Path), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlitedb.Attach(t.Context(), opts); err == nil {
		t.Fatal("attach created a database")
	}

	if _, err := os.Stat(opts.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database file after failed attach: %v", err)
	}
}

func TestOpenRejectsSharedDirectory(t *testing.T) {
	opts := testOptions(t)
	if err := os.Mkdir(filepath.Dir(opts.Path), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlitedb.Open(t.Context(), opts); !errors.Is(err, sqlitedb.ErrSharedDirectory) {
		t.Fatalf("open error = %v, want ErrSharedDirectory", err)
	}
}

func TestOpenRejectsInvalidOptions(t *testing.T) {
	for name, mutate := range map[string]func(*sqlitedb.Options){
		"relative path":       func(o *sqlitedb.Options) { o.Path = "state.db" },
		"no readers":          func(o *sqlitedb.Options) { o.ReadConns = 0 },
		"no idle limit":       func(o *sqlitedb.Options) { o.ConnMaxIdleTime = 0 },
		"no busy timeout":     func(o *sqlitedb.Options) { o.BusyTimeout = 0 },
		"no journal limit":    func(o *sqlitedb.Options) { o.JournalSizeLimit = 0 },
		"no checkpoint cycle": func(o *sqlitedb.Options) { o.CheckpointInterval = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t)
			mutate(&opts)

			if _, err := sqlitedb.Open(t.Context(), opts); err == nil {
				t.Fatal("open succeeded")
			}
		})
	}
}

func TestIdleConnectionsAreClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opts := testOptions(t)

		opts.ConnMaxIdleTime = 20 * time.Millisecond

		db := openTest(t, opts)

		for _, pool := range []*sql.DB{db.Writer(), db.Reader()} {
			if err := pool.PingContext(t.Context()); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(3 * time.Second)
		synctest.Wait()

		if writer, reader := db.Writer().Stats().OpenConnections, db.Reader().Stats().OpenConnections; writer != 0 || reader != 0 {
			t.Fatalf("idle connections stayed open: writer=%d reader=%d", writer, reader)
		}
	})
}

func TestCheckpointTruncatesWAL(t *testing.T) {
	opts := testOptions(t)
	db := openTest(t, opts)

	if _, err := db.Writer().ExecContext(t.Context(), "CREATE TABLE t (v BLOB) STRICT"); err != nil {
		t.Fatal(err)
	}

	for range 50 {
		if _, err := db.Writer().ExecContext(t.Context(), "INSERT INTO t VALUES (randomblob(4096))"); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(opts.Path + "-wal")
	if err != nil {
		t.Fatal(err)
	}

	if info.Size() != 0 {
		t.Fatalf("wal size after truncate = %d", info.Size())
	}
}

func TestWriteTxReleasesTheWriterAfterAPanic(t *testing.T) {
	db := openTest(t, testOptions(t))

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("write tx swallowed the panic")
			}
		}()

		if err := db.WriteTx(t.Context(), func(*sql.Tx) error { panic("stop") }); err != nil {
			t.Errorf("write tx returned %v instead of panicking", err)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := db.WriteTx(ctx, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("writer after a panic: %v", err)
	}
}

func TestWriteTxRollsBackOnError(t *testing.T) {
	db := openTest(t, testOptions(t))
	ctx := t.Context()

	if _, err := db.Writer().ExecContext(ctx, "CREATE TABLE t (v INTEGER) STRICT"); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("stop")
	err := db.WriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO t VALUES (1)"); err != nil {
			return fmt.Errorf("insert: %w", err)
		}

		return failure
	})

	if !errors.Is(err, failure) {
		t.Fatalf("write tx error = %v", err)
	}

	var count int

	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("rows after rollback = %d", count)
	}
}

func TestWriteTxReportsTheDeadlineWhileWaitingForTheWriter(t *testing.T) {
	opts := testOptions(t)
	db := openTest(t, opts)

	other, err := sqlitedb.Attach(t.Context(), opts)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := other.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close attached: %v", closeErr)
		}
	})

	holder, err := other.Writer().Conn(t.Context())
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}

	defer func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Errorf("close holder: %v", closeErr)
		}
	}()

	if _, beginErr := holder.ExecContext(t.Context(), "BEGIN IMMEDIATE"); beginErr != nil {
		t.Fatalf("hold writer lock: %v", beginErr)
	}

	defer func() {
		if _, rollbackErr := holder.ExecContext(context.WithoutCancel(t.Context()), "ROLLBACK"); rollbackErr != nil {
			t.Errorf("release writer lock: %v", rollbackErr)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()

	err = db.WriteTx(ctx, func(*sql.Tx) error { return nil })

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write while locked = %v, want context.DeadlineExceeded in the chain", err)
	}

	if elapsed := time.Since(started); elapsed >= opts.BusyTimeout {
		t.Fatalf("write waited %s, want the context deadline before busy timeout %s", elapsed, opts.BusyTimeout)
	}
}

func TestMigrateAppliesOnceAndRejectsDrift(t *testing.T) {
	opts := testOptions(t)
	db := openTest(t, opts)
	ctx := t.Context()

	fsys := fstest.MapFS{
		"m/0001_create.sql": {Data: []byte("CREATE TABLE t (v INTEGER) STRICT;")},
		"m/0002_seed.sql":   {Data: []byte("INSERT INTO t VALUES (1);")},
		"m/README.md":       {Data: []byte("ignored")},
	}

	migrations, err := sqlitedb.LoadMigrations(fsys, "m")
	if err != nil {
		t.Fatal(err)
	}

	if len(migrations) != 2 || migrations[0].Name != "0001_create.sql" {
		t.Fatalf("loaded migrations = %+v", migrations)
	}

	for range 2 {
		if err := db.Migrate(ctx, migrations); err != nil {
			t.Fatal(err)
		}
	}

	var count int

	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("seed applied %d times", count)
	}

	edited := []sqlitedb.Migration{migrations[0], {Name: migrations[1].Name, SQL: "INSERT INTO t VALUES (2);"}}
	if err := db.Migrate(ctx, edited); !errors.Is(err, sqlitedb.ErrMigrationDrift) {
		t.Fatalf("drift error = %v", err)
	}

	if err := db.Migrate(ctx, migrations[:1]); !errors.Is(err, sqlitedb.ErrUnknownMigration) {
		t.Fatalf("unknown migration error = %v", err)
	}

	failing := slices.Concat(migrations, []sqlitedb.Migration{{Name: "0003_broken.sql", SQL: "INSERT INTO t VALUES (3); INSERT INTO missing VALUES (1);"}})
	if err := db.Migrate(ctx, failing); err == nil {
		t.Fatal("broken migration succeeded")
	}

	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("broken migration left %d rows", count)
	}
}
