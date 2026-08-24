package dbmigrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestApplyIdempotentCoreHasNoHiddenState(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		testFirstSQL: {Data: []byte("create table if not exists a(id int)")},
		testSecondSQL: {
			Data: []byte("create index if not exists a_id_idx on a(id)"),
		},
	}

	var got []string

	exec := func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}

	if err := Apply(t.Context(), fsys, exec); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}

	if err := Apply(t.Context(), fsys, exec); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	want := []string{
		"create table if not exists a(id int)",
		"create index if not exists a_id_idx on a(id)",
		"create table if not exists a(id int)",
		"create index if not exists a_id_idx on a(id)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("executed SQL = %v, want %v", got, want)
	}
}

func TestApplyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fsys    fs.FS
		exec    Execer
		wantErr string
	}{
		{
			name: "nil exec",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 first.sql\n")},
				testFirstSQL: {Data: []byte("select 1")},
			},
			wantErr: "exec is required",
		},
		{
			name: "read missing sql",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 missing.sql\n")},
			},
			exec:    func(context.Context, string, ...any) error { return nil },
			wantErr: "read missing.sql",
		},
		{
			name: "exec error names file",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 first.sql\n")},
				testFirstSQL: {Data: []byte("select 1")},
			},
			exec:    func(context.Context, string, ...any) error { return errors.New("boom") },
			wantErr: "exec first.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Apply(t.Context(), tt.fsys, tt.exec)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Apply() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestSQLExec(t *testing.T) {
	t.Parallel()

	db := sql.OpenDB(&fakeSQLConnector{})

	defer func() { _ = db.Close() }()

	if err := SQLExec(db)(t.Context(), "select 1 where $1 = $1", 1); err != nil {
		t.Fatalf("SQLExec() error = %v", err)
	}
}

type fakeSQLConnector struct{}

func (c *fakeSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeSQLConn{}, nil
}

func (c *fakeSQLConnector) Driver() driver.Driver {
	return fakeSQLDriver{}
}

type fakeSQLDriver struct{}

func (d fakeSQLDriver) Open(string) (driver.Conn, error) {
	return &fakeSQLConn{}, nil
}

type fakeSQLConn struct{}

func (c *fakeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *fakeSQLConn) Close() error {
	return nil
}

func (c *fakeSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin not supported")
}

func (c *fakeSQLConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func TestApplyRunsEachMigrationFileWithoutArguments(t *testing.T) {
	t.Parallel()

	body := "CREATE TABLE a(id int);\nCREATE TABLE b(id int);\n"
	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte(body)},
	}

	var calls []recordedExec

	if err := Apply(t.Context(), fsys, recordingExec(&calls, "")); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	var applied *recordedExec

	for i := range calls {
		if calls[i].query == body {
			applied = &calls[i]

			break
		}
	}

	if applied == nil {
		t.Fatalf("migration body was never executed; calls = %v", calls)
	}

	if len(applied.args) != 0 {
		t.Fatalf("migration body executed with %d arguments (%v); pgx only takes the simple protocol — and with it the implicit transaction that makes a multi-statement file atomic — when the argument list is empty", len(applied.args), applied.args)
	}
}

func TestApplyWithOnlyRejectsEmptySelection(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName:  {Data: []byte("001 first.sql\n002 second.sql\n")},
		testFirstSQL:  {Data: []byte("select 1")},
		testSecondSQL: {Data: []byte("select 2")},
	}

	db := newFakeLedgerDB()

	err := Apply(t.Context(), fsys, db.Exec,
		WithOnly(),
		WithSession(SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Second}),
		WithLedger(Ledger{}, db),
	)
	if err == nil {
		t.Fatal("Apply() error = nil, want error for empty WithOnly selection")
	}

	if !strings.Contains(err.Error(), "WithOnly") {
		t.Fatalf("Apply() error = %v, want error naming WithOnly", err)
	}

	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want none: empty WithOnly must fail before session/ledger side effects", db.execs)
	}

	if len(db.events) != 0 {
		t.Fatalf("events = %v, want none before WithOnly validation", db.events)
	}
}

func TestApplyWithOnlyEmptyVariadicSliceRejected(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte("select 1")},
	}

	var (
		selected []string
		got      []string
	)

	err := Apply(t.Context(), fsys, func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}, WithOnly(selected...))
	if err == nil {
		t.Fatal("Apply() error = nil, want error for empty WithOnly selection")
	}

	if len(got) != 0 {
		t.Fatalf("executed SQL = %v, want none", got)
	}
}
